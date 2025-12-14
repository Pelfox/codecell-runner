FROM mcr.microsoft.com/dotnet/sdk:10.0-alpine

# Disable telemetry & first-time experience
ENV DOTNET_CLI_TELEMETRY_OPTOUT=1
ENV DOTNET_SKIP_FIRST_TIME_EXPERIENCE=1
ENV NUGET_XMLDOC_MODE=skip

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
