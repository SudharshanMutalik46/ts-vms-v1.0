$items = @('Config', 'Controls', 'Images', 'Models', 'Resources', 'Services', 'ViewModels', 'Views', 'App.xaml', 'App.xaml.cs', 'TSVmsDesktop.csproj')
$src = 'c:\Users\sudha\Desktop\ts_vms_1.0\desktop\TSVmsDesktop'
$dest = 'c:\Users\sudha\Desktop\TSVmsDesktop_Source_Phase3.9.zip'
$temp = Join-Path $env:TEMP 'TSVmsBundle_Final'

if (Test-Path $temp) { Remove-Item -Path $temp -Recurse -Force }
New-Item -ItemType Directory -Path $temp -Force | Out-Null

foreach ($item in $items) {
    $itemPath = Join-Path $src $item
    if (Test-Path $itemPath) {
        Copy-Item -Path $itemPath -Destination $temp -Recurse -Force
    }
}

# Ensure no build artifacts got in
Get-ChildItem -Path $temp -Recurse -Include 'bin', 'obj' | Remove-Item -Recurse -Force

# Create the archive
Compress-Archive -Path (Join-Path $temp '*') -DestinationPath $dest -Force

# Cleanup temp
Remove-Item -Path $temp -Recurse -Force

Write-Host "Bundle created successfully at $dest"
