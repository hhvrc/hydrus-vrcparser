using HydrusTagger.Cli.Parity;

// Placeholder entry point. The real System.CommandLine surface arrives with the
// CLI step; for now this exposes only the parity harness used to validate the
// port against the legacy Python implementation.
if (args.Length >= 3 && args[0] == "dump-chunk-parse")
{
    return ChunkParseDumper.Run(databasePath: args[1], outputPath: args[2]);
}

if (args.Length >= 3 && args[0] == "dump-file-tags")
{
    return FileTagDumper.Run(databasePath: args[1], outputPath: args[2]);
}

Console.Error.WriteLine("""
    usage:
      hydrus-tagger dump-chunk-parse <database> <output.jsonl>
      hydrus-tagger dump-file-tags   <database> <output.jsonl>
    """);
return 1;
