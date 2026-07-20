package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/google/go-github/v89/github"
	"github.com/joho/godotenv"
)

func main() {
	godotenv.Load()
	myProjectItems("DPGAlliance", 14)
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
	pat := os.Getenv("GITHUB_PAT")
	client, err := github.NewClient(github.WithAuthToken(pat))
	if err != nil {
		panic(err)
	}

	ctx := context.Background()
	opts := &github.RepositoryListByOrgOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}

	for {
		repos, resp, err := client.Repositories.ListByOrg(ctx, "DPGAlliance", opts)
		if err != nil {
			panic(err)
		}

		for _, repo := range repos {
			fmt.Println(repo.GetFullName())
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
}

// graphQL runs a GraphQL query against the GitHub API and unmarshals the
// response's data into out. It panics on transport or GraphQL errors.
func graphQL(query string, variables map[string]any, out any) {
	pat := os.Getenv("GITHUB_PAT")

	body, _ := json.Marshal(map[string]any{
		"query":     query,
		"variables": variables,
	})

	req, err := http.NewRequest(http.MethodPost, "https://api.github.com/graphql", bytes.NewReader(body))
	if err != nil {
		panic(err)
	}
	req.Header.Set("Authorization", "Bearer "+pat)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		panic(err)
	}
	if len(envelope.Errors) > 0 {
		panic(envelope.Errors[0].Message)
	}
	if err := json.Unmarshal(envelope.Data, out); err != nil {
		panic(err)
	}
}

// myProjectItems lists the items in org project #number that are assigned to
// the PAT owner (the GraphQL "viewer").
func myProjectItems(org string, number int) {
	const query = `query($org:String!, $number:Int!, $cursor:String) {
	  viewer { login }
	  organization(login:$org) {
	    projectV2(number:$number) {
	      title
	      items(first:100, after:$cursor) {
	        pageInfo { hasNextPage endCursor }
	        nodes {
	          content {
	            __typename
	            ... on Issue        { title url state updatedAt assignees(first:10){ nodes { login } } }
	            ... on PullRequest  { title url state updatedAt assignees(first:10){ nodes { login } } }
	            ... on DraftIssue   { title       updatedAt assignees(first:10){ nodes { login } } }
	          }
	        }
	      }
	    }
	  }
	}`

	type content struct {
		Typename  string    `json:"__typename"`
		Title     string    `json:"title"`
		URL       string    `json:"url"`
		State     string    `json:"state"`
		UpdatedAt time.Time `json:"updatedAt"`
		Assignees struct {
			Nodes []struct {
				Login string `json:"login"`
			} `json:"nodes"`
		} `json:"assignees"`
	}
	type resp struct {
		Viewer struct {
			Login string `json:"login"`
		} `json:"viewer"`
		Organization struct {
			ProjectV2 struct {
				Title string `json:"title"`
				Items struct {
					PageInfo struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
					Nodes []struct {
						Content content `json:"content"`
					} `json:"nodes"`
				} `json:"items"`
			} `json:"projectV2"`
		} `json:"organization"`
	}

	since := time.Now().Add(-24 * time.Hour)

	var cursor *string
	var login, title string
	found := 0
	for {
		var out resp
		graphQL(query, map[string]any{"org": org, "number": number, "cursor": cursor}, &out)

		login = out.Viewer.Login
		title = out.Organization.ProjectV2.Title
		items := out.Organization.ProjectV2.Items

		for _, item := range items.Nodes {
			c := item.Content
			if c.Typename == "" { // e.g. redacted item the PAT can't see
				continue
			}
			assigned := false
			for _, a := range c.Assignees.Nodes {
				if a.Login == login {
					assigned = true
					break
				}
			}
			if !assigned {
				continue
			}
			if c.UpdatedAt.Before(since) {
				continue
			}
			found++
			state := c.State
			if state == "" {
				state = "DRAFT"
			}
			url := c.URL
			if url == "" {
				url = "(draft)"
			}
			fmt.Printf("[%s]\t%s\t%s\t%s\n", c.UpdatedAt.Local().Format("2006-01-02 15:04"), state, c.Title, url)
		}

		if !items.PageInfo.HasNextPage {
			break
		}
		cursor = &items.PageInfo.EndCursor
	}

	fmt.Printf("\n%d item(s) assigned to %s updated in the last 24h in project #%d %q\n", found, login, number, title)
}
