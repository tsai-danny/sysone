package sysone_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/tsai-danny/sysone"
)

// stubServer serves a canned /v1/systemone response so the examples run
// without a live llama.cpp server.
func stubServer(body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}))
}

func ExampleClient_Decide() {
	srv := stubServer(`{
	  "model": "ggml-org/Kev-4B-GGUF",
	  "answers": {
	    "route":  {"type":"choice","choice":"billing","probabilities":{"billing":0.90,"shipping":0.03,"technical":0.07},"confidence":0.86},
	    "angry":  {"type":"noul","noul":0.82}
	  },
	  "usage": {"input_tokens":130,"output_tokens":0}
	}`)
	defer srv.Close()

	client, err := sysone.NewClient(sysone.Config{BaseURL: srv.URL + "/v1/systemone"})
	if err != nil {
		panic(err)
	}

	resp, err := client.Decide().
		WithTextState("Customer message: I was charged twice and I am furious!").
		AddChoice(sysone.ChoiceQ{
			ID: "route", Instructions: "Which team should handle this?",
			Options: []sysone.Option{
				{Key: "billing", Description: "payments, refunds, invoices"},
				{Key: "shipping", Description: "delivery status, tracking"},
				{Key: "technical", Description: "bugs, errors, crashes"},
			},
		}).
		AddNoul(sysone.NoulQ{ID: "angry", Instructions: "Is the customer angry?", Threshold: 0.8}).
		Execute(context.Background())
	if err != nil {
		panic(err)
	}

	fmt.Println("route:", resp.Choice("route"))
	fmt.Println("angry:", resp.Answers["angry"].Passed)
	fmt.Println("all passed:", resp.Passed())
	// Output:
	// route: billing
	// angry: true
	// all passed: true
}

func ExampleClient_Ask() {
	srv := stubServer(`{
	  "model": "ggml-org/Kev-4B-GGUF",
	  "answers": {"ok": {"type":"noul","noul":0.91}},
	  "usage": {"input_tokens":40,"output_tokens":0}
	}`)
	defer srv.Close()

	client, err := sysone.NewClient(sysone.Config{BaseURL: srv.URL + "/v1/systemone"})
	if err != nil {
		panic(err)
	}

	resp, err := client.Ask(context.Background(), "Build output: 3 tests passed, 0 failed.", map[string]sysone.Question{
		"ok": sysone.Noul{QID: "ok", Instructions: "Did the build succeed?"},
	})
	if err != nil {
		panic(err)
	}
	fmt.Println("ok:", resp.Noul("ok") >= 0.5)
	// Output:
	// ok: true
}

func ExampleClient_Health() {
	srv := stubServer(`{"status":"ok"}`)
	defer srv.Close()

	client, err := sysone.NewClient(sysone.Config{BaseURL: srv.URL + "/v1/systemone"})
	if err != nil {
		panic(err)
	}
	if err := client.Health(context.Background()); err != nil {
		panic(err)
	}
	fmt.Println("healthy")
	// Output:
	// healthy
}

func ExampleResponse() {
	srv := stubServer(`{
	  "model": "ggml-org/Kev-4B-GGUF",
	  "answers": {
	    "urgency": {"type":"score","score":2.28,"legend":{"0":"can wait","1":"this week","2":"today","3":"right now"},"probabilities":{"0":0.04,"1":0.19,"2":0.22,"3":0.55},"confidence":0.28}
	  },
	  "usage": {"input_tokens":100,"output_tokens":0}
	}`)
	defer srv.Close()

	client, _ := sysone.NewClient(sysone.Config{BaseURL: srv.URL + "/v1/systemone"})
	resp, err := client.Decide().
		WithTextState("My package has been stuck for two weeks.").
		AddScore(sysone.ScoreQ{ID: "urgency", Instructions: "How urgent is this?", Levels: []string{"can wait", "this week", "today", "right now"}}).
		Execute(context.Background())
	if err != nil {
		panic(err)
	}
	fmt.Printf("score: %.2f of %d levels\n", resp.Score("urgency"), 4)
	// Output:
	// score: 2.28 of 4 levels
}
