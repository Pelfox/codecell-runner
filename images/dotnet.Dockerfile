FROM mcr.microsoft.com/dotnet/sdk:10.0.101-alpine3.23

# Disable telemetry & first-time experience
ENV DOTNET_EnableDiagnostics=0 \
    DOTNET_CLI_TELEMETRY_OPTOUT=1 \
    DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1 \
    DOTNET_SKIP_FIRST_TIME_EXPERIENCE=1 \
    NUGET_XMLDOC_MODE=skip

# Create runner user
RUN addgroup -S runner && adduser -S runner -G runner

WORKDIR /workspace

RUN chown runner:runner /workspace

# Create a dummy project to warm NuGet cache
RUN dotnet new console -n Warmup -f net10.0 && \
    cd Warmup && \
    dotnet restore && \
    cd .. && \
    rm -rf Warmup

USER runner

CMD ["dotnet", "--info"]
