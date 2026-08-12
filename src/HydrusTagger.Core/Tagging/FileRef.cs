namespace HydrusTagger.Core.Tagging;

/// <summary>
/// Identity of a Hydrus file: enough to locate it on disk and to address it in
/// the API, without carrying its full metadata.
/// </summary>
/// <remarks>
/// The host resolves these from its own cache where possible, so a run that has
/// nothing to do issues no <c>file_metadata</c> requests at all.
/// </remarks>
public sealed record FileRef(int FileId, string Hash, string Ext)
{
    /// <summary>
    /// Path relative to a Hydrus data directory, following its
    /// <c>f&lt;hash[:2]&gt;/&lt;hash&gt;.&lt;ext&gt;</c> layout.
    /// </summary>
    public string RelativePath
    {
        get
        {
            if (Hash.Length < 2)
            {
                throw new InvalidOperationException(
                    $"File {FileId} has hash '{Hash}', too short to form a Hydrus path.");
            }

            return Path.Combine($"f{Hash[..2]}", $"{Hash}.{Ext}");
        }
    }

    /// <summary>Absolute path under a given Hydrus data directory.</summary>
    public string PathUnder(string dataDir) => Path.Combine(dataDir, RelativePath);
}
