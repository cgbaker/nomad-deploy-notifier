package stream

import (
	"context"
	"os"

	"github.com/cgbaker/nomad-deploy-notifier/internal/bot"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad/api"
)

type Stream struct {
	approverID string
	nomad *api.Client
	L     hclog.Logger
}

func NewStream(approverID string, client *api.Client) *Stream {
	return &Stream{
		approverID: approverID,
		nomad: client,
		L:     hclog.Default(),
	}
}

func (s *Stream) Subscribe(ctx context.Context, slack *bot.Bot) {
	events := s.nomad.EventStream()

	topics := map[api.Topic][]string{
		api.Topic("Job"): {"*"},
	}

	_, meta, err := s.nomad.Jobs().List(nil)
	if err != nil {
		s.L.Error("error listing jobs to get initial index")
		os.Exit(1)
	}

	s.L.Info("subscribing to event stream as approver", "approver_id", s.approverID, "index", meta.LastIndex)
	eventCh, err := events.Stream(ctx, topics, meta.LastIndex, &api.QueryOptions{})
	if err != nil {
		s.L.Error("error creating event stream client", "error", err)
		os.Exit(1)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case event := <-eventCh:
			if event.Err != nil {
				s.L.Warn("error from event stream", "error", err)
				break
			}
			if event.IsHeartbeat() {
				continue
			}

			// HACK: unfortunately, initial insertion of job will create two events in the stream
			// one for the insertion into job_versions, one for the insertion into jobs
			// de-dupe these here
			jobs := map[string]*api.Job{}
			for _, e := range event.Events {
				if e.Type != "JobRegistered" {
					s.L.Debug("skipping message", "type", e.Type)
					continue
				}
				job, err := e.Job()
				if err != nil {
					s.L.Error("expected job", "error", err)
					continue
				}
				if job == nil {
					s.L.Error("job was nil")
					continue
				}
				if job.Version == nil || *job.Version != 1000 {
					s.L.Info("not pending job", "job", *job.ID, "job", *job.Version)
					continue
				}
				if len(job.Approvers) == 0 {
					s.L.Info("job did not need approval", "job", *job.ID)
					continue
				}
				if job.Status == nil || *job.Status != "triage" {
					s.L.Info("job not in triage status", "job", *job.ID, "status", *job.Status)
					continue
				}
				if job.Approvers[0] != s.approverID {
					s.L.Info("not next approver", "job", *job.ID, "next_approver", job.Approvers[0])
					continue
				}
				jobs[*job.ID] = job
			}
			for _, job := range jobs {
				s.L.Info("processing job", "job", *job.ID, "status", *job.Status)
				if err = slack.UpsertJobMsg(job); err != nil {
					s.L.Warn("error from bot", "error", err)
					return
				}
			}
		}
	}
}
