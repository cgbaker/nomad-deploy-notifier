package bot

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/hashicorp/go-hclog"
	nomadapi "github.com/hashicorp/nomad/api"
	"github.com/slack-go/slack"
)

type Config struct {
	ApproverSecret string
	Token   string
	Channel string
}

type approvalMessage struct {
	job *nomadapi.Job
	ts  string
}

type Bot struct {
	mu        sync.Mutex
	chanID    string
	api       *slack.Client
	approvals map[string]*approvalMessage
	L         hclog.Logger
	nomadClient *nomadapi.Client
	nomadApproverSecret string
}

func NewBot(cfg Config, client *nomadapi.Client) (*Bot, error) {
	if cfg.Token == "" {
		return nil, fmt.Errorf("no token provided")
	}

	api := slack.New(cfg.Token, slack.OptionDebug(false))

	bot := &Bot{
		api:       api,
		chanID:    cfg.Channel,
		approvals: make(map[string]*approvalMessage),
		L:         hclog.Default(),
		nomadClient: client,
		nomadApproverSecret: cfg.ApproverSecret,
	}

	return bot, nil
}

func (b *Bot) HandleApproval(callback *slack.InteractionCallback) {
	if len(callback.ActionCallback.AttachmentActions) != 1 {
		b.L.Warn("unexpected action callback")
		return
	}
	action := callback.ActionCallback.AttachmentActions[0].Name
	switch action {
	case "approve", "deny":
	default:
		b.L.Warn("unexpected action value", "value", action)
		return
	}
	jobId := callback.CallbackID
	b.L.Info("received callback", "action", action, "job", jobId)

	b.mu.Lock()
	defer b.mu.Unlock()

	approval := b.approvals[jobId]
	if approval == nil {
		b.L.Warn("received callback for non-existent approval", "job", jobId)
	}

	// approve nomad job
	if err := b.approveJob(approval, &callback.User, action); err != nil {
		b.L.Error("error updating nomad job", "error", err)
		return
	}

	if err := b.updatedMessage(callback, approval, action); err != nil {
		b.L.Error("error updating slack message", "error", err)
		return
	}

	delete(b.approvals, jobId)
}

func (b *Bot) approveJob(approval *approvalMessage, user *slack.User, action string) error {
	job := approval.job
	opts := &nomadapi.RegisterOptions{
		PolicyOverride: false,
		PreserveCounts: false,
		Admission: &nomadapi.AdmissionPayload{
			Secret: b.nomadApproverSecret,
		},
	}
	switch action {
	case "approve":
		if job.Meta == nil {
			job.Meta = map[string]string{}
		}
		job.Meta["SLACK_APPROVER"] = user.Name
	case "deny":
		opts.Admission.Error = fmt.Sprintf("approval rejected by %s", user.Name)
	}
	_, _, err := b.nomadClient.Jobs().RegisterOpts(job, opts, nil)
	return err
}

func (b *Bot) updatedMessage(callback *slack.InteractionCallback, approval *approvalMessage, action string) error {
	msg := callback.OriginalMessage
	fields := []slack.AttachmentField{}
	if len(msg.Attachments) == 1 {
		fields = msg.Attachments[0].Fields
	}
	fields = append(
		fields,
		slack.AttachmentField{
			Title: "Approver",
			Value: callback.User.Name,
		},
		slack.AttachmentField{
			Title: "Action",
			Value: action,
		},
	)
	attachments := []slack.Attachment{
		{
			Fallback:   "job registration complete",
			Title:      fmt.Sprintf("Job Registration (%s)", action),
			TitleLink: fmt.Sprintf("http://localhost:4646/ui/jobs/%s",*approval.job.ID),
			Fields:     fields,
			Footer:     fmt.Sprintf("Job ID: %s", *approval.job.ID),
		},
	}
	b.L.Info("updating slack message", "job", *approval.job.ID)
	_, _, _, err := b.api.UpdateMessage(b.chanID, approval.ts, slack.MsgOptionAttachments(attachments...))
	return err
}

func (b *Bot) UpsertJobMsg(job *nomadapi.Job) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	jobId := *job.ID
	approval := b.approvals[jobId]
	if approval != nil {
		b.L.Info("deleting existing message")
		b.api.DeleteMessage(b.chanID, approval.ts)
	}

	b.L.Info("Sending approval message for job", "job", *job.ID)
	return b.sendInitialMessage(job)
}

func (b *Bot) sendInitialMessage(job *nomadapi.Job) error {
	plan, _, _ := b.nomadClient.Jobs().Plan(job, true, nil)
	attachments := initialAttachments(job, plan)

	opts := []slack.MsgOption{slack.MsgOptionAttachments(attachments...)}
	opts = append(opts)

	b.L.Info("sending message to slack", "job", *job.ID)
	_, ts, err := b.api.PostMessage(b.chanID, opts...)
	if err != nil {
		return err
	}
	b.approvals[*job.ID] = &approvalMessage{
		job: job,
		ts:  ts,
	}
	return nil
}

func initialAttachments(job *nomadapi.Job, plan *nomadapi.JobPlanResponse) []slack.Attachment {
	actions := []slack.AttachmentAction{
		{
			Name: "approve",
			Text: "Approve :heavy_check_mark:",
			Type: "button",
		},
		{
			Name:  "deny",
			Text:  "Deny :no_entry_sign:",
			Style: "danger",
			Type:  "button",
		},
	}
	var fields []slack.AttachmentField
	for _, tg := range job.TaskGroups {
		for _, task := range tg.Tasks {
			var cmd string
			if task.Driver == "docker" {
				cmd = "Image: " + getString("image", task.Config)
			} else {
				cmd = "Command: " + getString("command", task.Config) + " " + getString("args", task.Config)
			}
			field := slack.AttachmentField{
				Title: fmt.Sprintf("Task: %s/%s", *tg.Name, task.Name),
				Value: fmt.Sprintf("Driver: %s, %v", task.Driver, cmd),
			}
			fields = append(fields, field)
		}
	}
	if plan != nil {
		for _, p := range plan.Diff.Fields {
			fields = append(fields, slack.AttachmentField{
				Title: p.Name,
				Value: fmt.Sprintf("%q -> %q", p.Old, p.New),
			})
		}
	}
	return []slack.Attachment{
		{
			Fallback:   "job registration",
			Title:      "Job Registration Approval",
			TitleLink: fmt.Sprintf("http://localhost:4646/ui/jobs/%s/versions",*job.ID),
			Fields:     fields,
			Footer:     fmt.Sprintf("Job ID: %s", *job.ID),
			Ts:         json.Number(fmt.Sprintf("%d", time.Now().Unix())),
			Actions:    actions,
			CallbackID: *job.ID,
		},
	}
}

func getString(key string, config map[string]interface{}) (val string) {
	if raw, ok := config[key]; ok {
		if i, ok := raw.(string); ok {
			val = i
		}
	}
	return
}

