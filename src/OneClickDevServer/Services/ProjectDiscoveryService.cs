using OneClickDevServer.Models;

namespace OneClickDevServer.Services;

public sealed class ProjectDiscoveryService
{
    private static readonly (string Path, string Kind)[] KnownRoots =
    {
        (@"C:\xampp\htdocs", "XAMPP"),
        (@"C:\laragon\www", "Laragon")
    };

    public IReadOnlyList<ProjectCandidate> Discover()
    {
        var projects = new List<ProjectCandidate>();

        foreach (var (root, kind) in KnownRoots)
        {
            if (!Directory.Exists(root))
            {
                continue;
            }

            foreach (var directory in Directory.EnumerateDirectories(root))
            {
                var info = new DirectoryInfo(directory);

                // Never follow directory junctions/symlinks during automatic discovery.
                if (info.Attributes.HasFlag(FileAttributes.ReparsePoint))
                {
                    continue;
                }

                projects.Add(new ProjectCandidate(info.Name, info.FullName, kind));
            }
        }

        return projects
            .OrderBy(project => project.SourceKind)
            .ThenBy(project => project.Name, StringComparer.OrdinalIgnoreCase)
            .ToArray();
    }
}
