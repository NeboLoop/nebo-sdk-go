package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	nebo "github.com/neboloop/nebo-sdk-go"
)

type Calculator struct{}

func (c *Calculator) Name() string        { return "calculator" }
func (c *Calculator) Description() string { return "Performs arithmetic calculations." }

func (c *Calculator) Schema() json.RawMessage {
	return nebo.NewSchema("add", "subtract", "multiply", "divide").
		Number("a", "First operand", true).
		Number("b", "Second operand", true).
		Build()
}

func (c *Calculator) Execute(_ context.Context, input json.RawMessage) (string, error) {
	var in struct {
		Action string  `json:"action"`
		A      float64 `json:"a"`
		B      float64 `json:"b"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	var result float64
	switch in.Action {
	case "add":
		result = in.A + in.B
	case "subtract":
		result = in.A - in.B
	case "multiply":
		result = in.A * in.B
	case "divide":
		if in.B == 0 {
			return "", fmt.Errorf("division by zero")
		}
		result = in.A / in.B
	default:
		return "", fmt.Errorf("unknown action: %s", in.Action)
	}

	return fmt.Sprintf("%g %s %g = %g", in.A, in.Action, in.B, result), nil
}

func main() {
	app, err := nebo.New()
	if err != nil {
		log.Fatal(err)
	}
	app.RegisterTool(&Calculator{})
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
