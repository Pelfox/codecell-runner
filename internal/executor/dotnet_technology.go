package executor

import (
	"os"
	"path"
)

type DotNetTechnology struct{}

func (t DotNetTechnology) GetCommand() []string {
	return []string{"dotnet", "run"}
}

func (t DotNetTechnology) GetImage() string {
	return "codecell/dotnet"
}

func (t DotNetTechnology) GetSteps() []PrepareStep {
	// TODO: maybe rewrite in the future...
	writeProjectStep := func(workspaceDir string, sourceCode string) error {
		contents := `
<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <OutputType>Exe</OutputType>
    <TargetFramework>net10.0</TargetFramework>
    <ImplicitUsings>enable</ImplicitUsings>
    <Nullable>enable</Nullable>
  </PropertyGroup>
</Project>
`
		return os.WriteFile(path.Join(workspaceDir, "Runner.csproj"), []byte(contents), 0644)
	}

	writeCodeStep := func(workspaceDir string, sourceCode string) error {
		return os.WriteFile(path.Join(workspaceDir, "Program.cs"), []byte(sourceCode), 0644)
	}

	return []PrepareStep{
		writeProjectStep,
		writeCodeStep,
	}
}
