package prompter

import (
	_ "embed"
	"os"
)

//go:embed prompts/category_path.txt
var categoryPathPrompt string

//go:embed prompts/profession.txt
var professionPrompt string

//go:embed prompts/moderation.txt
var moderationPrompt string

func Init(promptFile string) error {
	if promptFile == "" {
		return nil
	}

	data, err := os.ReadFile(promptFile)
	if err != nil {
		return err
	}

	professionPrompt = string(data)
	return nil
}
