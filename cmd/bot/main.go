package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/hashicorp/nomad/api"
	"github.com/slack-go/slack"

	"github.com/cgbaker/nomad-deploy-notifier/internal/bot"
	"github.com/cgbaker/nomad-deploy-notifier/internal/stream"
)

func main() {
	os.Exit(realMain(os.Args))
}

func realMain(args []string) int {
	ctx, closer := CtxWithInterrupt(context.Background())
	defer closer()

	approverID := os.Getenv("NOMAD_APPROVER_ID")
	approverSecret := os.Getenv("NOMAD_APPROVER_SECRET")
	if approverSecret == "" {
		fmt.Println("must have approver secret")
		os.Exit(1)
	}

	token := os.Getenv("SLACK_TOKEN")
	toChannel := os.Getenv("SLACK_CHANNEL")

	slackCfg := bot.Config{
		ApproverSecret: approverSecret,
		Token:   token,
		Channel: toChannel,
	}

	nomadClient, err := api.NewClient(&api.Config{})
	if err != nil {
		panic(err)
	}
	stream := stream.NewStream(approverID, nomadClient)

	slackBot, err := bot.NewBot(slackCfg, nomadClient)
	if err != nil {
		panic(err)
	}

	http.HandleFunc("/", actionHandler(slackBot))
	fmt.Println("[INFO] Server listening")
	go http.ListenAndServe(":80", nil)

	stream.Subscribe(ctx, slackBot)

	return 0
}

func actionHandler(slackBot *bot.Bot) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload slack.InteractionCallback
		if err := json.Unmarshal([]byte(r.FormValue("payload")), &payload); err != nil {
			fmt.Printf("Could not parse action response JSON: %v\n", err)
			return
		}
		slackBot.HandleApproval(&payload)
	}
}

func CtxWithInterrupt(ctx context.Context) (context.Context, func()) {

	ctx, cancel := context.WithCancel(ctx)

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		select {
		case <-ch:
			cancel()
		case <-ctx.Done():
			return
		}
	}()

	return ctx, func() {
		signal.Stop(ch)
		cancel()
	}
}
