namespace OneClickDevServer.Models;

public sealed record SandboxPlan(
    ProjectCandidate Project,
    string SandboxId,
    string VmName,
    string SandboxDirectory,
    string ChildDiskPath,
    string TransferDiskPath,
    string BaseImagePath);
