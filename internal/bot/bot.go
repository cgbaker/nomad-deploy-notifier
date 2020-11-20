package bot

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad/api"
	"github.com/slack-go/slack"
)

type Config struct {
	ApproverID string
	ApproverSecret string
	Token   string
	Channel string
}

type Bot struct {
	mu               sync.Mutex
	chanID           string
	api              *slack.Client
	approvalMessages map[string]string
	L                hclog.Logger
}

func NewBot(cfg Config) (*Bot, error) {
	if cfg.Token == "" {
		return nil, fmt.Errorf("no token provided")
	}

	api := slack.New(cfg.Token)

	bot := &Bot{
		api:              api,
		chanID:           cfg.Channel,
		approvalMessages: make(map[string]string),
		L: hclog.Default(),
	}

	return bot, nil
}

func (b *Bot) UpsertJobMsg(job api.Job) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.initialMsg(job)
	// ts, ok := b.approvalMessages[*job.ID]
	// if !ok {
	// 	return b.initialMsg(job)
	// }
	// b.L.Debug("Existing job found, updating status", "slack ts", ts)
	//
	// attachments := DefaultAttachments(job)
	// opts := []slack.MsgOption{slack.MsgOptionAttachments(attachments...)}
	// opts = append(opts, DefaultMsgOpts()...)
	//
	// _, ts, _, err := b.api.UpdateMessage(b.chanID, ts, opts...)
	// if err != nil {
	// 	return err
	// }
	// b.approvalMessages[*job.ID] = ts
	//
	// return nil
}

func (b *Bot) initialMsg(job api.Job) error {
	attachments := DefaultAttachments(job)

	opts := []slack.MsgOption{slack.MsgOptionAttachments(attachments...)}
	opts = append(opts, DefaultMsgOpts()...)

	b.L.Info("sending message to slack")
	_, ts, err := b.api.PostMessage(b.chanID, opts...)
	if err != nil {
		return err
	}
	b.approvalMessages[*job.ID] = ts
	return nil
}

func DefaultMsgOpts() []slack.MsgOption {
	return []slack.MsgOption{
		slack.MsgOptionAsUser(true),
	}
}

func DefaultAttachments(job api.Job) []slack.Attachment {
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
			Confirm: &slack.ConfirmationField{
				Title:       "Are you sure?",
				Text:        ":cry: :cry: :cry: :cry: :cry:",
				OkText:      "Fail",
				DismissText: "Whoops!",
			},
		},
	}
	var fields []slack.AttachmentField
	for _, tg := range job.TaskGroups {
		for _, task := range tg.Tasks {
			field := slack.AttachmentField{
				Title: fmt.Sprintf("Task: %s/%s", tg.Name, task.Name),
				Value: fmt.Sprintf("Driver: %s\n%#v", task.Driver, task.Config),
			}
			fields = append(fields, field)
		}
	}
	return []slack.Attachment{
		{
			Fallback:   "job registration",
			Title:      "Job Registration Approval",
			Fields:     fields,
			Footer:     fmt.Sprintf("Job ID: %s", job.ID),
			Ts:         json.Number(fmt.Sprintf("%d", time.Now().Unix())),
			Actions:    actions,
			CallbackID: *job.ID,
		},
	}
}

