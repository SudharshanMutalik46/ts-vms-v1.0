using System;
using System.Diagnostics;
using System.IO;

class Program
{
    [STAThread]
    static int Main()
    {
        try
        {
            var baseDir = AppDomain.CurrentDomain.BaseDirectory;
            var launcher = Path.Combine(baseDir, "scripts", "Start-TSVmsDemo.cmd");
            if (!File.Exists(launcher))
            {
                Console.Error.WriteLine("Launcher not found: " + launcher);
                return 1;
            }

            var desktop = Environment.GetFolderPath(Environment.SpecialFolder.DesktopDirectory);
            var shortcutPath = Path.Combine(desktop, "TS VMS Demo.lnk");
            CreateShortcut(shortcutPath, launcher, baseDir);

            Process.Start(new ProcessStartInfo
            {
                FileName = launcher,
                WorkingDirectory = baseDir,
                UseShellExecute = true
            });

            Console.WriteLine("Desktop shortcut created: " + shortcutPath);
            Console.WriteLine("TS-VMS Demo started.");
            return 0;
        }
        catch (Exception ex)
        {
            Console.Error.WriteLine(ex.ToString());
            return 2;
        }
    }

    static void CreateShortcut(string shortcutPath, string targetPath, string workingDirectory)
    {
        var shellType = Type.GetTypeFromProgID("WScript.Shell");
        if (shellType == null)
        {
            throw new InvalidOperationException("WScript.Shell COM object is not available.");
        }

        dynamic shell = Activator.CreateInstance(shellType);
        dynamic shortcut = shell.CreateShortcut(shortcutPath);
        shortcut.TargetPath = targetPath;
        shortcut.WorkingDirectory = workingDirectory;
        shortcut.Description = "Start TS VMS Demo";
        shortcut.IconLocation = Path.Combine(workingDirectory, "app", "desktop", "TSVmsDesktop.exe");
        shortcut.Save();
    }
}
