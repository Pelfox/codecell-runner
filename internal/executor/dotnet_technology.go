package executor

import (
	"io"

	"github.com/Pelfox/codecell-runner/pkg"
)

const projectConfigContents = `
<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <OutputType>Exe</OutputType>
    <TargetFramework>net10.0</TargetFramework>
    <ImplicitUsings>enable</ImplicitUsings>
    <Nullable>enable</Nullable>
  </PropertyGroup>
</Project>
`

type DotNetTechnology struct{}

func (t DotNetTechnology) GetCommand() []string {
	return []string{"dotnet", "run"}
}

func (t DotNetTechnology) GetImage() string {
	return "codecell/dotnet"
}

func (t DotNetTechnology) WriteSourceCode(sourceCode string) (io.Reader, error) {
	return pkg.CreateTar(map[string][]byte{
		"Runner.csproj": []byte(projectConfigContents),
		"Program.cs":    []byte(sourceCode),
	})
}
