// Package systemone is a type-safe Go client for the llama.cpp System One
// decision-model API (POST /v1/systemone, TypeSafe/Jev-compatible).
//
// Decision models answer typed questions about a state in a single forward
// pass: no token generation, no streaming. Every answer is one of the options
// you supplied, together with probabilities.
//
// # Question types
//
//   - choice: pick one option from a labelled set (criteria = option → description).
//   - score:  expected level index (fractional) over 2–10 levels ordered low → high.
//   - noul:   probability that a yes/no question is true.
//
// # Recommended flow: the Step Builder
//
// [Client.Decide] enforces the correct assembly order at compile time
// (state first, then questions, then execute) and binds answers into
// caller-provided pointers:
//
//	client, err := systemone.NewClient(systemone.Config{})
//	if err != nil { log.Fatal(err) }
//
//	var route string
//	var urgent bool
//	ans, err := client.Decide().
//		WithTextState("Customer message: I was charged twice for my order.").
//		AddChoice("route", "Which team should handle this?", map[string]string{
//			"billing": "payments, refunds", "technical": "bugs, errors",
//		}, &route).
//		AddNoul("urgent", "Is this urgent?", 0.8, &urgent).
//		Execute(ctx)
//
// # Escape hatch
//
// [Client.Ask] sends wire-level question definitions directly, mirroring the
// flat shape of the official TypeSafe SDKs.
//
// # Errors
//
// Local validation failures wrap [ErrInvalidRequest] (or [ErrNoQuestions]) and
// carry a "Hint:" line. Remote failures surface as [*APIError] (OpenAI-style
// code/message/type) and are reachable with errors.As.
//
// # Configuration
//
// [Config] fields fall back to the environment variables SYSTEMONE_URL,
// SYSTEMONE_API_KEY and SYSTEMONE_MODEL.
package systemone
