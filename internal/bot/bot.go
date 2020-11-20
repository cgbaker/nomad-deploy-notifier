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
	ApproverID string
	ApproverSecret string
	Token   string
	Channel string
}

type Bot struct {
	mu        sync.Mutex
	chanID    string
	api       *slack.Client
	approvals map[string]*nomadapi.Job
	L         hclog.Logger
}

func NewBot(cfg Config) (*Bot, error) {
	if cfg.Token == "" {
		return nil, fmt.Errorf("no token provided")
	}

	api := slack.New(cfg.Token, slack.OptionDebug(false))

	bot := &Bot{
		api:       api,
		chanID:    cfg.Channel,
		approvals: make(map[string]*nomadapi.Job),
		L:         hclog.Default(),
	}

	return bot, nil
}

func (b *Bot) HandleApproval(callback *slack.InteractionCallback) *slack.Message {
	if len(callback.ActionCallback.AttachmentActions) != 1 {
		b.L.Warn("unexpected action callback")
		return nil
	}
	action := callback.ActionCallback.AttachmentActions[0].Name
	switch action {
	case "approve", "deny":
	default:
		b.L.Warn("unexpected action value", "value", action)
		return nil
	}
	jobId := callback.CallbackID
	b.L.Info("received callback", "action", action, "job", jobId)

	b.mu.Lock()
	defer b.mu.Unlock()

	job := b.approvals[jobId]
	if job == nil {
		b.L.Warn("received callback for non-existent approval", "job", jobId)
	}
	return nil
}

func (b *Bot) UpsertJobMsg(job *nomadapi.Job) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.L.Info("Sending approval message for job", "job", *job.ID)
	return b.initialMsg(job)
}

func (b *Bot) initialMsg(job *nomadapi.Job) error {
	attachments := DefaultAttachments(job)

	opts := []slack.MsgOption{slack.MsgOptionAttachments(attachments...)}
	opts = append(opts)

	b.L.Info("sending message to slack")
	_, _, err := b.api.PostMessage(b.chanID, opts...)
	if err != nil {
		return err
	}
	b.approvals[*job.ID] = job
	return nil
}

func DefaultAttachments(job *nomadapi.Job) []slack.Attachment {
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
	return []slack.Attachment{
		{
			Fallback:   "job registration",
			Title:      "Job Registration Approval",
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

