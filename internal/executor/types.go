package executor

import "io"

type Technology interface {
	GetImage() string
	GetCommand() []string
	WriteSourceCode(sourceCode string) (io.Reader, error)
}
