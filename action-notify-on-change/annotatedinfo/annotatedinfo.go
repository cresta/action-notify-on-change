package annotatedinfo

import "context"

type AnnotatedInfo struct {
	ChangedFiles []string
	LinkToChange string
	LinkToAuthor string
	PrCreator    string
	PrBase       string
}

type Populator interface {
	Populate(ctx context.Context) (*AnnotatedInfo, error)
}
