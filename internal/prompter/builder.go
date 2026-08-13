package prompter

import "fmt"

func GigPrompt() string {
	return professionPrompt
}

func ModerationPrompt() string {
	return moderationPrompt
}

func CategoryPathPrompt(catalog string) string {
	if catalog == "" {
		catalog = "(empty)"
	}
	return fmt.Sprintf(categoryPathPrompt, catalog)
}
