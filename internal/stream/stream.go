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

func NewStream(approverID string) *Stream {
	client, _ := api.NewClient(&api.Config{})
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

	s.L.Info("subscribing to event stream as approver", "approver_id", s.approverID)
	eventCh, err := events.Stream(ctx, topics, 0, &api.QueryOptions{})
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

			for _, e := range event.Events {
				if e.Type != "JobRegistered" {
					s.L.Info("skipping message", "type", e.Type)
					continue
				}
				job, err := e.Job()
				if err != nil {
					s.L.Error("expected job", "error", err)
					continue
				}
				if job == nil {
					continue
				}
				if len(job.Approvers) == 0 {
					s.L.Error("job did not need approval", "job", *job.ID)
					continue
				}
				if job.Approvers[0] != s.approverID {
					s.L.Error("not next approver", "job", *job.ID, "next_approver", job.Approvers[0])
					continue
				}

				if err = slack.UpsertJobMsg(*job); err != nil {
					s.L.Warn("error from bot", "error", err)
					return
				}
			}
		}
	}
}
