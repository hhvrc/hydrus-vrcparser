namespace HydrusTagger.Taggers.Vrchat;

public sealed class VrchatTaggerOptions
{
    public const string SectionName = "Vrchat";

    /// <summary>
    /// Hydrus data directory holding the files, e.g. <c>\\pve\hydrus\files</c>.
    /// Used only when a file has no directory recorded against it.
    /// </summary>
    public string DataDirectory { get; set; } = "";

    /// <summary>Hydrus search predicates selecting candidate screenshots.</summary>
    public IReadOnlyList<string> SelectorQuery { get; set; } =
        ["system:filetype is png", "system:has embedded metadata"];
}
