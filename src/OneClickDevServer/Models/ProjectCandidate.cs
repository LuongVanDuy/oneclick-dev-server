namespace OneClickDevServer.Models;

public sealed record ProjectCandidate(
    string Name,
    string FullPath,
    string SourceKind)
{
    public string DisplayName => $"{Name}  —  {SourceKind}";
}
