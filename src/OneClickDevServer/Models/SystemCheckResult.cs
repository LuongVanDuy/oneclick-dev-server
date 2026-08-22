namespace OneClickDevServer.Models;

public sealed record SystemCheckResult(
    bool IsWindows,
    bool IsAdministrator,
    bool HyperVEnabled,
    bool VirtualizationAvailable,
    string Summary)
{
    public bool CanDeploy => IsWindows && IsAdministrator && HyperVEnabled && VirtualizationAvailable;
}
