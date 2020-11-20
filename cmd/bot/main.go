package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

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
	if toChannel == "" {
		toChannel = "nomad-testing"
	}
	if token == "" {
		token = ""
	}

	slackCfg := bot.Config{
		ApproverID: approverID,
		ApproverSecret: approverSecret,
		Token:   token,
		Channel: toChannel,
	}

	stream := stream.NewStream(approverID)

	slackBot, err := bot.NewBot(slackCfg)
	if err != nil {
		panic(err)
	}

	stream.Subscribe(ctx, slackBot)

	return 0
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
