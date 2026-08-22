using System.Security.Principal;
using OneClickDevServer.Models;

namespace OneClickDevServer.Services;

public sealed class SafetyCheckService(PowerShellRunner powerShell)
{
    public async Task<SystemCheckResult> RunAsync(CancellationToken cancellationToken = default)
    {
        var isWindows = OperatingSystem.IsWindows();
        var isAdministrator = false;

        if (isWindows)
        {
            using var identity = WindowsIdentity.GetCurrent();
            var principal = new WindowsPrincipal(identity);
            isAdministrator = principal.IsInRole(WindowsBuiltInRole.Administrator);
        }

        if (!isWindows)
        {
            return new(false, isAdministrator, false, false, "OneClick Dev Server currently requires Windows with Hyper-V.");
        }

        var hyperV = await powerShell.RunAsync(
            "(Get-WindowsOptionalFeature -Online -FeatureName Microsoft-Hyper-V-All).State",
            cancellationToken);

        var virtualization = await powerShell.RunAsync(
            "$c = Get-CimInstance Win32_Processor | Select-Object -First 1; if ($c.VirtualizationFirmwareEnabled) { 'True' } else { 'False' }",
            cancellationToken);

        var hyperVEnabled = hyperV.ExitCode == 0 && hyperV.StdOut.Contains("Enabled", StringComparison.OrdinalIgnoreCase);
        var virtualizationAvailable = virtualization.ExitCode == 0 && virtualization.StdOut.Contains("True", StringComparison.OrdinalIgnoreCase);

        var summary = hyperVEnabled && virtualizationAvailable
            ? "Host prerequisites look ready. Untrusted projects can be prepared for isolated VM execution."
            : "Isolation prerequisites are incomplete. Deployment must remain blocked until Hyper-V and firmware virtualization are enabled.";

        return new(isWindows, isAdministrator, hyperVEnabled, virtualizationAvailable, summary);
    }
}
