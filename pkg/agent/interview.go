package agent

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"
	"zerotwo/pkg/provider"
)

const interviewSystemPrompt = `You are conducting a structured profile interview. Your goal is to build a comprehensive, accurate profile of the person you are interviewing — one that will be used to personalise a personal AI agent called ZeroTwo so it can effectively streamline their tasks, communications, and decision-making.

## Interview Principles
- Ask ONE focused question at a time. Never ask multiple questions in one turn.
- Listen carefully to answers. Extract facts, preferences, patterns, and values.
- Ask natural follow-ups when an answer is interesting or incomplete.
- Be conversational, warm, and concise.
- When you have covered all areas sufficiently, signal completion by outputting exactly: INTERVIEW_COMPLETE

## Sections to cover:
1. Identity & Role
2. Current Projects & Priorities
3. Communication Style (tone, verbosity, formats)
4. Key Tools, Stack, & Environment
5. Decision-Making & Constraints (pet peeves, things the agent should always/never do)

Start the interview now with a warm, brief introduction (2 sentences max) and your first question.`

const synthesisPrompt = `The interview is complete. Synthesise all information into a clean MEMORY.md file.

Write ONLY the markdown below. No preamble, no conversational filler.

# Profile
*Built: %s via structured interview*

## Identity
- Name: 
- Role / Domain: 

## Active Projects
<!-- Specific projects, goals, current status -->

## Communication Style
- Default tone: 
- Verbosity: 

## Preferences & Constraints
- Always do: 
- Never do: 

## Environment & Tech Stack
`

type Interviewer struct {
	client *provider.Client
	memory *MemoryStore
}

func NewInterviewer(client *provider.Client, memory *MemoryStore) *Interviewer {
	return &Interviewer{client: client, memory: memory}
}

func (iv *Interviewer) Run(ctx context.Context) error {
	fmt.Println("\n── ZeroTwo Profile Interview ──────────────────────────────")
	fmt.Println("This interview builds your personal profile for MEMORY.md.")
	fmt.Println("───────────────────────────────────────────────────────────")
	fmt.Println()

	messages := []provider.Message{
		{Role: "system", Content: interviewSystemPrompt},
	}

	reader := bufio.NewReader(os.Stdin)

	for turn := 0; turn < 25; turn++ {
		req := provider.CompletionRequest{
			Messages:    messages,
			MaxTokens:   512,
			Temperature: 0.7,
			Stream:      false,
		}

		resp, err := iv.client.Complete(ctx, req, nil)
		if err != nil {
			return fmt.Errorf("interviewer LLM error: %w", err)
		}

		assistantText := strings.TrimSpace(resp.Choices[0].Message.Content)

		if strings.Contains(assistantText, "INTERVIEW_COMPLETE") {
			fmt.Println("\nInterview complete! Synthesising your profile into MEMORY.md...")
			return iv.synthesize(ctx, messages)
		}

		fmt.Printf("\n\033[1mZeroTwo>\033[0m %s\n\nyou> ", assistantText)

		line, err := reader.ReadString('\n')
		if err != nil {
			return nil // EOF
		}

		userInput := strings.TrimSpace(line)
		if userInput == "quit" || userInput == "exit" {
			fmt.Println("Interview cancelled.")
			return nil
		}
		if userInput == "" {
			userInput = "(skipped)"
		}

		messages = append(messages,
			provider.Message{Role: "assistant", Content: assistantText},
			provider.Message{Role: "user", Content: userInput},
		)
	}

	fmt.Println("\nInterview concluded. Synthesising profile...")
	return iv.synthesize(ctx, messages)
}

func (iv *Interviewer) synthesize(ctx context.Context, history []provider.Message) error {
	today := time.Now().Format("2006-01-02")
	prompt := fmt.Sprintf(synthesisPrompt, today)

	messages := append(history, provider.Message{
		Role:    "user",
		Content: prompt,
	})

	req := provider.CompletionRequest{
		Messages:    messages,
		MaxTokens:   2048,
		Temperature: 0.3,
		Stream:      false,
	}

	resp, err := iv.client.Complete(ctx, req, nil)
	if err != nil {
		return fmt.Errorf("synthesis error: %w", err)
	}

	memoryContent := strings.TrimSpace(resp.Choices[0].Message.Content)
	if err := iv.memory.Save(memoryContent); err != nil {
		return fmt.Errorf("saving synthesized memory: %w", err)
	}

	fmt.Printf("✓ Profile successfully saved to %s\n", iv.memory.Path())
	return nil
}
