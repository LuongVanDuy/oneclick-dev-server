using System.Windows;
using OneClickDevServer.Models;
using OneClickDevServer.Services;

namespace OneClickDevServer;

public partial class MainWindow : Window
{
    private readonly SafetyCheckService _safetyCheckService = new(new PowerShellRunner());
    private readonly ProjectDiscoveryService _projectDiscoveryService = new();

    public MainWindow()
    {
        InitializeComponent();
        RefreshProjects();
    }

    private void RefreshProjectsButton_Click(object sender, RoutedEventArgs e) => RefreshProjects();

    private void RefreshProjects()
    {
        try
        {
            var projects = _projectDiscoveryService.Discover();
            ProjectsList.ItemsSource = projects;
            StatusBox.Text = projects.Count == 0
                ? "No projects found under C:\\xampp\\htdocs or C:\\laragon\\www."
                : $"Found {projects.Count} project(s). Discovery only reads directory metadata; it does not execute project code.";
        }
        catch (Exception ex)
        {
            StatusBox.Text = $"Project discovery failed: {ex.Message}";
        }
    }

    private void ProjectsList_SelectionChanged(object sender, System.Windows.Controls.SelectionChangedEventArgs e)
    {
        if (ProjectsList.SelectedItem is ProjectCandidate project)
        {
            SelectedPathText.Text = project.FullPath;
            CreateEnvironmentButton.IsEnabled = true;
        }
        else
        {
            SelectedPathText.Text = "Select a project.";
            CreateEnvironmentButton.IsEnabled = false;
        }
    }

    private async void CreateEnvironmentButton_Click(object sender, RoutedEventArgs e)
    {
        if (ProjectsList.SelectedItem is not ProjectCandidate project)
        {
            return;
        }

        CreateEnvironmentButton.IsEnabled = false;
        StatusBox.Text = $"Validating Hyper-V isolation before creating a dedicated VM for {project.Name}...";

        try
        {
            var result = await _safetyCheckService.RunAsync();
            if (!result.CanDeploy)
            {
                StatusBox.Text = $"Isolation prerequisites are not ready. No project code was started.\n\n{result.Summary}";
                return;
            }

            StatusBox.Text =
                $"Project selected: {project.FullPath}\n" +
                "Isolation prerequisites are ready. VM provisioning is the next implementation step; the host project folder will not be mounted into the guest.";
        }
        catch (Exception ex)
        {
            StatusBox.Text = $"Could not validate the isolation boundary. No project code was started.\n\n{ex.Message}";
        }
        finally
        {
            CreateEnvironmentButton.IsEnabled = ProjectsList.SelectedItem is ProjectCandidate;
        }
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
            StatusBox.Text = $"Hyper-V check failed. Environment creation remains blocked.\n\n{ex.Message}";
        }
        finally
        {
            RunCheckButton.IsEnabled = true;
        }
    }
}
