using System.Windows;
using OneClickDevServer.Services;

namespace OneClickDevServer;

public partial class MainWindow : Window
{
    private readonly SafetyCheckService _safetyCheckService = new(new PowerShellRunner());

    public MainWindow()
    {
        InitializeComponent();
    }

    private async void RunCheckButton_Click(object sender, RoutedEventArgs e)
    {
        RunCheckButton.IsEnabled = false;
        StatusBox.Text = "Checking Windows, administrator status, Hyper-V, and firmware virtualization...";

        try
        {
            var result = await _safetyCheckService.RunAsync();
            StatusBox.Text =
                $"Windows: {(result.IsWindows ? "OK" : "Missing")}\n" +
                $"Administrator: {(result.IsAdministrator ? "Yes" : "No")}\n" +
                $"Hyper-V: {(result.HyperVEnabled ? "Enabled" : "Not ready")}\n" +
                $"Firmware virtualization: {(result.VirtualizationAvailable ? "Available" : "Not ready")}\n\n" +
                result.Summary;
        }
        catch (Exception ex)
        {
            StatusBox.Text = $"Safety check failed. Deployment remains blocked.\n\n{ex.Message}";
        }
        finally
        {
            RunCheckButton.IsEnabled = true;
        }
    }
}
