using System;
using System.Threading.Tasks;
using Itexoft.Common.ExecutionTools;
using Itexoft.Common.Nuget;

namespace Atlas.Cli;

public class Program
{
    public async Task Main(string[] args)
    {
        await using var runner = new AtlasRunner();
        await runner.RunAsync(args);
    }
}

internal sealed class AtlasRunner() : ToolRunner(atlasPath, Environment.CurrentDirectory)
{
    private static readonly string atlasPath = NativeResolver.ResolveExePath("atlas");
}