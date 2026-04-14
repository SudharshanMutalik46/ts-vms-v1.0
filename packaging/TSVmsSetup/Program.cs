using System.Diagnostics;

var baseDir = AppContext.BaseDirectory;
var packageDir = Path.Combine(baseDir, "package");
if (!Directory.Exists(packageDir))
{
    Console.Error.WriteLine($"Package directory not found: {packageDir}");
    return 1;
}

var installRoot = Path.Combine(
    Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData),
    "TechnoSupport",
    "TS-VMS");

Console.WriteLine($"Installing TS-VMS into: {installRoot}");
CopyDirectory(packageDir, installRoot);

var launcherPath = Path.Combine(installRoot, "scripts", "Start-TSVmsPortable.cmd");
if (!File.Exists(launcherPath))
{
    Console.Error.WriteLine($"Launcher not found after install: {launcherPath}");
    return 2;
}

var desktopPath = Environment.GetFolderPath(Environment.SpecialFolder.DesktopDirectory);
var shortcutPath = Path.Combine(desktopPath, "TS VMS.lnk");
CreateShortcut(shortcutPath, launcherPath, installRoot);

Console.WriteLine($"Desktop shortcut created: {shortcutPath}");

var process = new Process
{
    StartInfo = new ProcessStartInfo
    {
        FileName = launcherPath,
        WorkingDirectory = installRoot,
        UseShellExecute = true
    }
};
process.Start();

Console.WriteLine("TS-VMS started.");
return 0;

static void CopyDirectory(string sourceDir, string destinationDir)
{
    Directory.CreateDirectory(destinationDir);

    foreach (var file in Directory.GetFiles(sourceDir))
    {
        var fileName = Path.GetFileName(file);
        var destinationPath = Path.Combine(destinationDir, fileName);
        File.Copy(file, destinationPath, true);
    }

    foreach (var directory in Directory.GetDirectories(sourceDir))
    {
        var directoryName = Path.GetFileName(directory);
        var destinationPath = Path.Combine(destinationDir, directoryName);
        CopyDirectory(directory, destinationPath);
    }
}

static void CreateShortcut(string shortcutPath, string targetPath, string workingDirectory)
{
    var shellType = Type.GetTypeFromProgID("WScript.Shell")
        ?? throw new InvalidOperationException("WScript.Shell COM object is not available.");

    dynamic shell = Activator.CreateInstance(shellType)
        ?? throw new InvalidOperationException("Failed to create WScript.Shell COM object.");

    dynamic shortcut = shell.CreateShortcut(shortcutPath);
    shortcut.TargetPath = targetPath;
    shortcut.WorkingDirectory = workingDirectory;
    shortcut.Description = "Start Techno Support VMS";
    shortcut.IconLocation = targetPath;
    shortcut.Save();
}
