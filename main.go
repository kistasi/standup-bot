package main

import (
	"context"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/google/go-github/v89/github"
	"github.com/joho/godotenv"
)

func main() {
	godotenv.Load()
	gh()
}

func claude() {
	client := anthropic.NewClient()

	message, err := client.Messages.New(context.TODO(), anthropic.MessageNewParams{
		MaxTokens: 1024,
		System: []anthropic.TextBlockParam{
			{Text: "Keep your answers short and concise."},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("Who plays Berta in the Two and a Half Men?")),
		},
		Model: anthropic.ModelClaudeOpus4_6,
	})
	if err != nil {
		panic(err.Error())
	}

	fmt.Printf("%+v\n", message.Content[0].Text)
}

func gh() {
	client, err := github.NewClient()
	if err != nil {
		panic(err)
	}

	orgs, _, err := client.Organizations.List(context.Background(), "kistasi", nil)
	if err != nil {
		panic(err)
	}

	fmt.Println(orgs)
}
