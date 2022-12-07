package annotatedinfo

import "context"

type AnnotatedInfo struct {
	ChangedFiles []string
	LinkToChange string
	LinkToAuthor string
	PrCreator    string
	PrBase       string
}

func (a *AnnotatedInfo) Populate(_ context.Context) (*AnnotatedInfo, error) {
	return a, nil
}

type Fetch interface {
	Populate(ctx context.Context) (*AnnotatedInfo, error)
}
