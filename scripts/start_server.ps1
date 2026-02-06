# Start TechnoSupport VMS Server (Console Mode)
$env:DB_HOST = "localhost"
$env:DB_USER = "postgres"
$env:DB_PASSWORD = "ts1234"
$env:DB_NAME = "ts_vms"
$env:REDIS_ADDR = "127.0.0.1:6379"
$env:JWT_SIGNING_KEY = "dev-secret-do-not-use-in-prod"
$env:MASTER_KEYS = '[{"kid":"dev-master-key","material":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="}]'
$env:ACTIVE_MASTER_KID = "dev-master-key"
$env:SFU_BASE_URL = "http://127.0.0.1:8085"
$env:SFU_SECRET = "sfu-internal-secret"
$env:MEDIA_PLANE_ADDR = "localhost:50051"
$env:AI_SERVICE_TOKEN = "dev_ai_secret"

Write-Host "Starting TS-VMS Server on localhost:8080..."
go run cmd/server/main.go
