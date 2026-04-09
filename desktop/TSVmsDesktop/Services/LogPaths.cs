using System;
using System.IO;

namespace TSVmsDesktop.Services
{
    public static class LogPaths
    {
        private static readonly string _rootPath = ResolveRepoRoot();
        private static readonly string _logsDir = Path.Combine(_rootPath, "logs");

        public static string ApiDebugLogPath
        {
            get
            {
                EnsureLogsDirectory();
                return Path.Combine(_logsDir, "api_debug_log.txt");
            }
        }

        public static string LogsDirectory
        {
            get
            {
                EnsureLogsDirectory();
                return _logsDir;
            }
        }

        private static void EnsureLogsDirectory()
        {
            if (!Directory.Exists(_logsDir))
            {
                Directory.CreateDirectory(_logsDir);
            }
        }

        private static string ResolveRepoRoot()
        {
            var baseDir = AppDomain.CurrentDomain.BaseDirectory;
            // bin/Debug/net8.0-windows -> project root is 6 levels up
            var root = Path.GetFullPath(Path.Combine(
                baseDir,
                "..",
                "..",
                "..",
                "..",
                "..",
                ".."));
            return root;
        }
    }
}
