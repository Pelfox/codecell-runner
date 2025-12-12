package executor

type PrepareStep func(workspaceDir string, sourceCode string) error

type Technology interface {
	GetImage() string
	GetCommand() []string
	GetSteps() []PrepareStep
}
