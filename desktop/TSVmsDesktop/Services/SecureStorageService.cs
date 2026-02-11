using System;
using System.IO;
using System.Security.Cryptography;
using System.Text;

namespace TSVmsDesktop.Services
{
    public interface ISecureStorageService
    {
        void SaveToken(string token);
        string? GetToken();
        void ClearToken();
    }

    public class SecureStorageService : ISecureStorageService
    {
        // Simple implementation for now. In a real app, use DPAPI or similar.
        private readonly string _filePath;

        public SecureStorageService()
        {
            var appData = Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData);
            var folder = Path.Combine(appData, "TS-VMS");
            Directory.CreateDirectory(folder);
            _filePath = Path.Combine(folder, "token.dat");
        }

        public void SaveToken(string token)
        {
            try
            {
                // Simple encoding for now, NOT secure encryption. 
                // TODO: upgrade to ProtectedData.Protect for production
                var bytes = Encoding.UTF8.GetBytes(token);
                var base64 = Convert.ToBase64String(bytes);
                File.WriteAllText(_filePath, base64);
            }
            catch (Exception ex)
            {
                Console.WriteLine($"Error saving token: {ex.Message}");
            }
        }

        public string? GetToken()
        {
            try
            {
                if (!File.Exists(_filePath)) return null;
                var base64 = File.ReadAllText(_filePath);
                var bytes = Convert.FromBase64String(base64);
                return Encoding.UTF8.GetString(bytes);
            }
            catch
            {
                return null;
            }
        }

        public void ClearToken()
        {
            try
            {
                if (File.Exists(_filePath))
                {
                    File.Delete(_filePath);
                }
            }
            catch (Exception ex)
            {
                Console.WriteLine($"Error clearing token: {ex.Message}");
            }
        }
    }
}
