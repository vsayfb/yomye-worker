package prompter

import "fmt"

func GigPrompt() string {
	return professionPrompt
}

func CategoryPathPrompt(catalog string) string {
	if catalog == "" {
		catalog = "(empty)"
	}
	return fmt.Sprintf(categoryPathPrompt, catalog)
}
