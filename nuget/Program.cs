using System;
using System.Threading.Tasks;
using Itexoft.Common.ExecutionTools;
using Itexoft.Common.Nuget;

namespace Atlas.Cli;

public class Program
{
    public async static Task<int> Main(string[] args)
    {
        await using var runner = new AtlasRunner();
        return await runner.RunAsync(args, Console.Out, Console.Error);
    }
}

internal sealed class AtlasRunner() : ToolRunner(atlasPath, Environment.CurrentDirectory)
{
    private static readonly string atlasPath = NativeResolver.ResolveToolExePath("atlas");
}