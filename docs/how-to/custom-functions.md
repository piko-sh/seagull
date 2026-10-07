---
title: Register custom SQL functions
description: Describe a function’s SQL name, argument types, return type and nullability
  in configuration or WithCustomFunctions. Generate a representative query using it
  and test against the real extension.
section: How-to guides
subsection: Embedding
order: 780
---

# Register custom SQL functions

Declare a SQL function’s signature so Seagull can type its calls. This guide uses an application-provided SQLite `slugify` function; declaring metadata does not implement the function in the database.

## Declare the function to the generator

Describe the function’s SQL name, argument and result types, and NULL behaviour:

```go
var slugifyFunction = seagull.CustomFunction{
	Name:         "slugify",
	ReturnType:   "text",
	Nullable:     "null_on_null",
	Arguments:    []string{"text"},
	MinArguments: 0,
	IsAggregate:  false,
	IsVariadic:   false,
}
```

Pass the declaration when constructing the generator:

```go
	generator, err := seagull.New(dialect_engine_sqlite.New(), seagull.WithCustomFunctions(functions...))
	if err != nil {
		return err
	}
```

`seagull.New` copies the declarations, their arguments included, so changing them afterwards does not change the generator. The [example’s driver setup](../../examples/embedding/048_custom_functions/main.go) registers the actual SQLite function separately. If your database already supplies the function, keep that implementation and declare only its metadata.

## Vet and call a representative query

Add a query that uses the function:

```sql
-- sgl.query(ListArticleSlugs, many)
SELECT id, title, slugify(title) AS slug FROM articles ORDER BY id;
```

Run `generator.Vet` on the project inputs, or configure the equivalent `custom_functions` entry and run `sgl vet`. Q005 should disappear and `slug` should be text. Generate the package and call the method:

```go
	articles, err := queries.ListArticleSlugs(ctx)
	if err != nil {
		return fmt.Errorf("listing slugs: %w", err)
	}
	for _, article := range articles {
		fmt.Fprintf(output, "%d\t%-28s %s\n", article.ID, article.Title, article.Slug)
	}

```

## Check the declared signature

From the Seagull repository root, run the custom-function example:

```sh
cd examples/embedding/048_custom_functions
go test .
```

The example test provides its local inputs. See its [caller](../../examples/embedding/048_custom_functions/example.go) for the exact Go API and its [test](../../examples/embedding/048_custom_functions/main_test.go) for assertions and failure handling.

The complete example produces:

```text
custom functions declared: 0
  articles.sql:5: warning[Q005]: unknown function "slugify"
  articles.sql:9: warning[Q005]: unknown function "slugify"
  ListArticleSlugs.slug: type unknown, nullable true
custom functions declared: 1
  ListArticleSlugs.slug: type text, nullable false
1	Gulls of the North Sea       gulls-of-the-north-sea
2	Why Chips?  A Field Guide    why-chips-a-field-guide
why-chips-a-field-guide is article 2
```

## Declare data access and NULL behaviour

A declared function is read-only by default. Set `DataAccess: "modifies_data"` (`data_access: modifies_data` in configuration) if it can write, so replica routing uses the writer. An undeclared function is conservatively treated as writing. Test the signature against the actual implementation, especially its behaviour with NULL arguments.

See the [configuration reference](../reference/configuration.md) for the complete contract. The [example browser](https://seagull.sh/examples/) exposes other engine variants; use their actual SQL rather than changing only the dialect name.
