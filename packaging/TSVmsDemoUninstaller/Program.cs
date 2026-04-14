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
            var desktop = Environment.GetFolderPath(Environment.SpecialFolder.DesktopDirectory);
            var shortcutPath = Path.Combine(desktop, "TS VMS Demo.lnk");
            if (File.Exists(shortcutPath))
            {
                File.Delete(shortcutPath);
            }

            // Stop only demo-owned processes; keep installation folder/files intact.
            var names = new[]
            {
                "redis-server",
                "nats-server",
                "server",
                "vms-control",
                "vms-media",
                "vms-mosaic",
                "vms-hlsd",
                "hlsd",
                "vms-recording-bin",
                "vms-recording",
                "node",
                "TSVmsDesktop"
            };
            foreach (var name in names)
            {
                foreach (var process in Process.GetProcessesByName(name))
                {
                    try { process.Kill(); }
                    catch { }
                }
            }

            Console.WriteLine("Desktop shortcut removed.");
            Console.WriteLine("Demo files were preserved.");
            return 0;
        }
        catch (Exception ex)
        {
            Console.Error.WriteLine(ex.ToString());
            return 1;
        }
    }
}
