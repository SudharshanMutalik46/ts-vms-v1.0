using System;
using System.Text;
using System.Security.Cryptography;
using System.IO;

namespace TSVmsDesktop.Services
{
    public interface ISecureStorageService
    {
        string? GetToken();
        void SetToken(string token);
        void ClearToken();
        string Encrypt(string plainText);
        string Decrypt(string cipherText);
    }

    public class SecureStorageService : ISecureStorageService
    {
        private readonly string _tokenFile;

        // Uses Windows DPAPI (Data Protection API)
        // Data is encrypted using the current user's credentials.
        // Only this user on this machine can decrypt it.

        public SecureStorageService()
        {
            string folder = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "TS-VMS");
            if (!Directory.Exists(folder)) Directory.CreateDirectory(folder);
            _tokenFile = Path.Combine(folder, "token.dat");
        }

        public string? GetToken()
        {
            if (!File.Exists(_tokenFile)) return null;
            try
            {
                byte[] cipherText = File.ReadAllBytes(_tokenFile);
                byte[] decrypted = ProtectedData.Unprotect(cipherText, null, DataProtectionScope.CurrentUser);
                return Encoding.UTF8.GetString(decrypted);
            }
            catch { return null; }
        }

        public void SetToken(string token)
        {
            try
            {
                byte[] plainText = Encoding.UTF8.GetBytes(token);
                byte[] encrypted = ProtectedData.Protect(plainText, null, DataProtectionScope.CurrentUser);
                File.WriteAllBytes(_tokenFile, encrypted);
            }
            catch { }
        }

        public void ClearToken()
        {
            if (File.Exists(_tokenFile)) File.Delete(_tokenFile);
        }

        public string Encrypt(string plainText)
        {
            if (string.IsNullOrEmpty(plainText)) return string.Empty;

            try
            {
                byte[] data = Encoding.UTF8.GetBytes(plainText);
                byte[] encrypted = ProtectedData.Protect(data, null, DataProtectionScope.CurrentUser);
                return Convert.ToBase64String(encrypted);
            }
            catch (Exception)
            {
                return string.Empty;
            }
        }

        public string Decrypt(string cipherText)
        {
            if (string.IsNullOrEmpty(cipherText)) return string.Empty;

            try
            {
                byte[] data = Convert.FromBase64String(cipherText);
                byte[] decrypted = ProtectedData.Unprotect(data, null, DataProtectionScope.CurrentUser);
                return Encoding.UTF8.GetString(decrypted);
            }
            catch (Exception)
            {
                return string.Empty;
            }
        }
    }
}
