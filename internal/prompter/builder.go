package prompter

import "fmt"

func BaseCategoryPrompt() string {
	return baseCategoryPrompt
}

func GigPrompt() string {
	return professionPrompt
}

func SalePrompt() string {
	return salePrompt
}

func CategoryPathPrompt(baseCategorySlug, catalog string) string {
	if catalog == "" {
		catalog = "(empty)"
	}
	return fmt.Sprintf(categoryPathPrompt, baseCategorySlug, catalog)
}
