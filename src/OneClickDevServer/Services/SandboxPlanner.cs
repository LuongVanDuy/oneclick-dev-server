using System.IO;
using System.Security.Cryptography;
using System.Text;
using OneClickDevServer.Models;

namespace OneClickDevServer.Services;

public sealed class SandboxPlanner
{
    private readonly string _dataRoot;

    public SandboxPlanner(string? dataRoot = null)
    {
        _dataRoot = dataRoot ?? Path.Combine(
            Environment.GetFolderPath(Environment.SpecialFolder.CommonApplicationData),
            "OneClickDevServer");
    }

    public SandboxPlan CreatePlan(ProjectCandidate project)
    {
        var id = BuildSandboxId(project);
        var sandboxDirectory = Path.Combine(_dataRoot, "sandboxes", id);

        return new SandboxPlan(
            project,
            id,
            $"ocds-{id}",
            sandboxDirectory,
            Path.Combine(sandboxDirectory, "system-diff.vhdx"),
            Path.Combine(sandboxDirectory, "transfer.vhdx"),
            Path.Combine(_dataRoot, "images", "base.vhdx"));
    }

    private static string BuildSandboxId(ProjectCandidate project)
    {
        var safeName = new string(project.Name
            .ToLowerInvariant()
            .Select(ch => char.IsLetterOrDigit(ch) ? ch : '-')
            .ToArray())
            .Trim('-');

        if (string.IsNullOrWhiteSpace(safeName))
        {
            safeName = "site";
        }

        safeName = safeName.Length > 28 ? safeName[..28] : safeName;

        var normalizedPath = Path.GetFullPath(project.FullPath)
            .TrimEnd(Path.DirectorySeparatorChar, Path.AltDirectorySeparatorChar)
            .ToUpperInvariant();

        var digest = SHA256.HashData(Encoding.UTF8.GetBytes(normalizedPath));
        var suffix = Convert.ToHexString(digest)[..10].ToLowerInvariant();

        return $"{safeName}-{suffix}";
    }
}
