using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;
using System.Threading.Tasks;
using System.Windows; 
using System.Collections.ObjectModel;
using TSVmsDesktop.Models;
using TSVmsDesktop.Services;
using System; // Added for DateTime

namespace TSVmsDesktop.ViewModels
{
    public partial class UsersViewModel : ObservableObject
    {
        private readonly UserService _userService;
        private readonly ISessionService _session;
        private readonly SettingsService _settings;

        // --- Workflow State ---
        [ObservableProperty] private bool _isCreatingUser;
        [ObservableProperty] private bool _isAssigningRole;
        [ObservableProperty] private bool _isVerifying;
        
        // --- Inputs ---
        [ObservableProperty] private string _searchUserId = "";
        [ObservableProperty] private string _newEmail = "";
        [ObservableProperty] private string _newDisplayName = "";
        [ObservableProperty] private string _newPassword = "";
        [ObservableProperty] private string _selectedRole = "viewer";
        [ObservableProperty] private string _tempPasswordDisplay = "";
        
        // Change Password
        [ObservableProperty] private string _currentPasswordInput = "";
        [ObservableProperty] private string _newPasswordInput = "";
        [ObservableProperty] private bool _isChangingPassword;

        // --- Data ---
        [ObservableProperty] private UserDto? _currentUser;
        [ObservableProperty] private UserDto? _selectedUser;
        [ObservableProperty] private string _statusMessage = "Ready";
        [ObservableProperty] private bool _isUserLoaded;
        [ObservableProperty] private bool _showDetails;
        [ObservableProperty] private bool _isEditingName; // New
        [ObservableProperty] private ObservableCollection<UserDto> _usersList = new();

        public ObservableCollection<string> AvailableRoles { get; } = new() { "admin", "operator", "viewer" };

        partial void OnCurrentUserChanged(UserDto? value)
        {
            IsUserLoaded = value != null;
            ShowDetails = value != null && !IsCreatingUser && !IsAssigningRole && !IsVerifying;
            if (value != null)
            {
                if (SearchUserId != value.Id)
                {
                    SearchUserId = value.Id;
                }
                StatusMessage = "User selected.";
            }
        }

        partial void OnSelectedUserChanged(UserDto? value)
        {
            if (value != null && !IsCreatingUser && !IsAssigningRole && !IsVerifying)
            {
                CurrentUser = value;
            }
        }

        public UsersViewModel(UserService userService, ISessionService session, SettingsService settings)
        {
            _userService = userService;
            _session = session;
            _settings = settings;
            _ = LoadUsers();
        }

        public async Task LoadUsers()
        {
            StatusMessage = "Loading users...";
            var users = await _userService.GetUsersAsync();
            UsersList.Clear();
            foreach (var user in users)
            {
                UsersList.Add(user);
            }
            StatusMessage = $"Loaded {users.Count} users.";
        }

        [RelayCommand]
        public async Task FindUser()
        {
            if (string.IsNullOrWhiteSpace(SearchUserId)) return;
            
            StatusMessage = "Searching...";
            var user = await _userService.GetUserAsync(SearchUserId);
            
            if (user != null)
            {
                CurrentUser = user;
                IsUserLoaded = true;
                StatusMessage = "User found.";
            }
            else
            {
                StatusMessage = "User not found.";
                IsUserLoaded = false;
                CurrentUser = null;
            }
        }

        // --- STEP 1: CREATE USER ---

        [RelayCommand]
        public void StartCreate()
        {
            // Reset fields
            NewEmail = "";
            NewDisplayName = "";
            NewPassword = ""; 
            IsCreatingUser = true;
            IsAssigningRole = false;
            IsVerifying = false;
            IsUserLoaded = false;
            ShowDetails = false;
            StatusMessage = "Enter details for new user.";
        }

        [RelayCommand]
        public async Task CreateUser()
        {
            if (string.IsNullOrWhiteSpace(NewEmail) || string.IsNullOrWhiteSpace(NewPassword))
            {
                StatusMessage = "Email and Password are required.";
                return;
            }

            StatusMessage = "Creating user...";
            
            var req = new CreateUserRequest
            {
                Email = NewEmail,
                DisplayName = NewDisplayName, // Optional but good practice
                Password = NewPassword,
                TenantId = _session.TenantId
            };

            string? newId = await _userService.CreateUserAsync(req);

            if (!string.IsNullOrEmpty(newId))
            {
                StatusMessage = $"User created (ID: {newId}). Proceed to Role Assignment.";
                
                
                // Automatically fetch the partial user to show context
                CurrentUser = new UserDto { Id = newId, Email = NewEmail, Username = NewEmail, DisplayName = NewDisplayName };
                
                // Move to Step 2
                IsCreatingUser = false;
                IsAssigningRole = true; 
                ShowDetails = false;
                
                // Refresh list in background
                _ = LoadUsers();
            }
            else
            {
                StatusMessage = "Error: Create failed. Check logs.";
                System.Windows.MessageBox.Show("Failed to create user. See logs.", "Error", System.Windows.MessageBoxButton.OK, System.Windows.MessageBoxImage.Error);
            }
        }

        [RelayCommand]
        public void CancelCreate()
        {
            IsCreatingUser = false;
            IsAssigningRole = false;
            IsVerifying = false;
            StatusMessage = "Cancelled.";
        }

        // --- STEP 2: ASSIGN ROLE ---

        [RelayCommand]
        public async Task AssignRole()
        {
            if (CurrentUser == null) return;

            StatusMessage = "Assigning role...";
            
            bool success = await _userService.AssignRoleAsync(CurrentUser.Id, SelectedRole, _session.TenantId);

            if (success)
            {
                StatusMessage = $"Role '{SelectedRole}' assigned successfully.";
                
                // Refresh the user object to confirm backend state
                var updated = await _userService.GetUserAsync(CurrentUser.Id);
                if (updated != null) CurrentUser = updated;

                // Move to Step 3 (Verification)
                IsAssigningRole = false;
                IsVerifying = true; 
                ShowDetails = false;
                TempPasswordDisplay = NewPassword; // Keep creds in memory for verification
            }
            else
            {
                StatusMessage = "Error: Failed to assign role.";
                System.Windows.MessageBox.Show("Role assignment failed. User is created but may have no role.", "Warning", System.Windows.MessageBoxButton.OK, System.Windows.MessageBoxImage.Warning);
            }
        }

        // --- STEP 3: VERIFY LOGIN ---

        [RelayCommand]
        public async Task TestLogin()
        {
            if (CurrentUser == null) return;

            StatusMessage = "Testing login with new credentials...";
            
            bool loginSuccess = await _userService.VerifyLoginAsync(
                CurrentUser.Email, 
                TempPasswordDisplay, 
                _session.TenantId, 
                _settings.CurrentSettings.BaseUrl
            );

            if (loginSuccess)
            {
                StatusMessage = "PASS: User can log in successfully.";
                System.Windows.MessageBox.Show("Verification PASSED.\n\nUser created, role assigned, and login verified.", "Success", System.Windows.MessageBoxButton.OK, System.Windows.MessageBoxImage.Information);
                
                // Done
                IsVerifying = false;
                IsUserLoaded = true; // Show final details
                ShowDetails = true;
            }
            else
            {
                StatusMessage = "FAIL: Login rejected.";
                System.Windows.MessageBox.Show("Verification FAILED.\n\nThe user cannot log in. Check password or tenant ID.", "Failure", System.Windows.MessageBoxButton.OK, System.Windows.MessageBoxImage.Error);
            }
        }

        [RelayCommand]
        public void FinishWizard()
        {
            IsVerifying = false;
            IsUserLoaded = true;
            ShowDetails = true;
            StatusMessage = "User setup complete.";
        }

        [RelayCommand]
        public async Task DisableUser()
        {
            if (CurrentUser == null) return;
            StatusMessage = "Disabling user...";
            bool success = await _userService.DisableUserAsync(CurrentUser.Id);
            if (success)
            {
                StatusMessage = "User disabled.";
                var updated = await _userService.GetUserAsync(CurrentUser.Id);
                if (updated != null) CurrentUser = updated;
            }
            else
            {
                StatusMessage = "Error: Failed to disable user.";
            }
        }

        [RelayCommand]
        public async Task EnableUser()
        {
            if (CurrentUser == null) return;
            StatusMessage = "Enabling user...";
            bool success = await _userService.EnableUserAsync(CurrentUser.Id);
            if (success)
            {
                StatusMessage = "User enabled.";
                var updated = await _userService.GetUserAsync(CurrentUser.Id);
                if (updated != null) CurrentUser = updated;
            }
            else
            {
                StatusMessage = "Error: Failed to enable user.";
            }
        }

        [RelayCommand]
        public async Task DeleteUser()
        {
            if (CurrentUser == null) return;

            var result = System.Windows.MessageBox.Show(
                $"Are you sure you want to delete user {CurrentUser.Email}? This action cannot be undone.",
                "Confirm Delete",
                System.Windows.MessageBoxButton.YesNo,
                System.Windows.MessageBoxImage.Warning);

            if (result != System.Windows.MessageBoxResult.Yes) return;

            StatusMessage = "Deleting user...";
            bool success = await _userService.DeleteUserAsync(CurrentUser.Id);
            if (success)
            {
                StatusMessage = "User deleted successfully.";
                CurrentUser = null;
                ShowDetails = false;
                await LoadUsers();
            }
            else
            {
                StatusMessage = "Error: Failed to delete user.";
                System.Windows.MessageBox.Show("Failed to delete user. You may not have permission or cannot delete yourself.", "Error", System.Windows.MessageBoxButton.OK, System.Windows.MessageBoxImage.Error);
            }
        }

        [RelayCommand]
        public void UpdateName()
        {
            if (CurrentUser == null) return;
            IsEditingName = true;
        }

        [RelayCommand]
        public async Task SaveName()
        {
            if (CurrentUser == null) return;
            
            StatusMessage = "Updating name...";
            bool success = await _userService.UpdateUserAsync(CurrentUser.Id, CurrentUser.DisplayName);
            
            if (success)
            {
                StatusMessage = "Name updated.";
                IsEditingName = false;
                await LoadUsers(); // Refresh list to show new name in sidebar
            }
            else
            {
                StatusMessage = "Error: Failed to update name.";
            }
        }

        [RelayCommand]
        public void EditName()
        {
        }

        [RelayCommand]
        public async Task ChangeOwnPassword()
        {
            // If NewPassword is empty, error
            if (string.IsNullOrWhiteSpace(NewPasswordInput))
            {
                StatusMessage = "Error: New password is required.";
                return;
            }

            // If OldPassword is provided -> Self-Service Change (Safe)
            if (!string.IsNullOrWhiteSpace(CurrentPasswordInput))
            {
                 // Safety Check: Cannot use Old Password for another user (backend changes caller's password)
                 if (CurrentUser?.Id != _session.CurrentUser?.Id)
                 {
                     StatusMessage = "Error: Cannot use 'Old Password' for another user. Leave empty for Admin Override.";
                     return;
                 }

                StatusMessage = "Changing password (verifying old)...";
                bool success = await _userService.ChangePasswordAsync(CurrentPasswordInput, NewPasswordInput);
                if (success)
                {
                    StatusMessage = "Password changed successfully.";
                    IsChangingPassword = false; 
                    CurrentPasswordInput = "";
                    NewPasswordInput = "";
                }
                else
                {
                    StatusMessage = "Error: Failed to change password. Check old password.";
                }
            }
            else
            {
                // If OldPassword is empty -> Admin Override (Force Set)
                // Only works if user has permission (backend checked)
                if (CurrentUser == null) return;
                
                StatusMessage = "Setting password (Admin Override)...";
                bool success = await _userService.SetPasswordAsync(CurrentUser.Id, NewPasswordInput);
                if (success)
                {
                    StatusMessage = "Password set successfully.";
                    IsChangingPassword = false;
                    CurrentPasswordInput = "";
                    NewPasswordInput = "";
                }
                else
                {
                    StatusMessage = "Error: Failed to set password. Permission denied?";
                }
            }
        }

        [RelayCommand]
        public void ToggleChangePassword()
        {
            IsChangingPassword = !IsChangingPassword;
            if (!IsChangingPassword)
            {
                CurrentPasswordInput = "";
                NewPasswordInput = "";
            }
        }

        [RelayCommand]
        public void CancelEditName()
        {
            IsEditingName = false;
            // Reload user to revert changes?
            _ = LoadUsers(); // crude revert relies on GetUserAsync or list
        }

        [RelayCommand]
        public void EditRole()
        {
            if (CurrentUser == null) return;
            IsCreatingUser = false;
            IsAssigningRole = true;
            IsVerifying = false;
            ShowDetails = false;
            StatusMessage = $"Editing role for {CurrentUser.Email}";
        }
    }
}
