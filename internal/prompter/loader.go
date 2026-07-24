package prompter

import (
	_ "embed"
	"os"
)

//go:embed prompts/profession.txt
var professionPrompt string

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
