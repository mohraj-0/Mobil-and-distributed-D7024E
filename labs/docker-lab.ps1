param(
    [ValidateSet('Prepare', 'Start', 'Shell', 'Status', 'Stop')]
    [string]$Action = 'Start',
    [string]$Stack = 'kadlab',
    [string]$Domain = 'example.com',
    [string]$OwnersFile = 'owners.json',
    [string]$PublisherKey = 'publisher.key'
)

$ErrorActionPreference = 'Stop'
Set-Location -LiteralPath $PSScriptRoot
if ($Stack -notmatch '^[a-zA-Z0-9][a-zA-Z0-9_-]*$') { throw 'Invalid stack name' }
if ($Domain -notmatch '^[a-z0-9][a-z0-9.-]*[a-z0-9]$') { throw 'Use a canonical lowercase domain' }
if (-not [IO.Path]::IsPathRooted($OwnersFile)) { $OwnersFile = Join-Path $PSScriptRoot $OwnersFile }
if (-not [IO.Path]::IsPathRooted($PublisherKey)) { $PublisherKey = Join-Path $PSScriptRoot $PublisherKey }
$OwnersFile = [IO.Path]::GetFullPath($OwnersFile)
$PublisherKey = [IO.Path]::GetFullPath($PublisherKey)

function Invoke-DockerChecked {
    param([string[]]$DockerArgs)
    & docker @DockerArgs
    if ($LASTEXITCODE -ne 0) { throw "Docker command failed: $($DockerArgs -join ' ')" }
}

function Prepare-Ownership {
    if (-not (Test-Path -LiteralPath $PublisherKey)) {
        $keyDirectory = [IO.Path]::GetDirectoryName($PublisherKey)
        [void][IO.Directory]::CreateDirectory($keyDirectory)
        $publicTXT = & go run ./cmd/simshell keygen $PublisherKey
        if ($LASTEXITCODE -ne 0) { throw 'Publisher key generation failed' }
    }
    # Existing keys are reused. Never print private key material or put it in a
    # Docker config, image, command argument, or environment variable.
    $keyText = [IO.File]::ReadAllText($PublisherKey).Trim()
    if ($keyText -notmatch '^[0-9a-fA-F]{128}$') { throw 'Expected a hex Ed25519 private-key file' }
    $publicKey = $keyText.Substring(64, 64).ToLowerInvariant()
    $keyText = $null
    $owners = @{}
    if (Test-Path -LiteralPath $OwnersFile) {
        $existing = [IO.File]::ReadAllText($OwnersFile) | ConvertFrom-Json
        if ($null -eq $existing -or $existing -isnot [PSCustomObject]) { throw 'Owners file must be a JSON object' }
        foreach ($property in $existing.PSObject.Properties) { $owners[$property.Name] = $property.Value }
    }
    if ($owners.ContainsKey($Domain)) {
        if ($owners[$Domain] -ne $publicKey) { throw "Existing owner for $Domain differs from the publisher key; configuration was preserved" }
    } else {
        $owners[$Domain] = $publicKey
        [void][IO.Directory]::CreateDirectory([IO.Path]::GetDirectoryName($OwnersFile))
        [IO.File]::WriteAllText($OwnersFile, ($owners | ConvertTo-Json), (New-Object Text.UTF8Encoding($false)))
    }
    Write-Host "Ownership ready: $Domain (public config: $OwnersFile)"
}

switch ($Action) {
    'Prepare' { Prepare-Ownership }
    'Start' {
        Prepare-Ownership
        Invoke-DockerChecked -DockerArgs @('build', '-t', 'kadlab:latest', '.')
        $swarmState = & docker info --format '{{.Swarm.LocalNodeState}}'
        if ($LASTEXITCODE -ne 0) { throw 'Docker Desktop Linux engine must be running' }
        if ([string]::IsNullOrWhiteSpace($swarmState)) { throw 'Could not read Docker Swarm state' }
        if ($swarmState.Trim() -eq 'inactive') { Invoke-DockerChecked -DockerArgs @('swarm', 'init') }
        # Swarm configs are immutable. A content-derived name permits ownership
        # changes on subsequent deployments without overwriting an active config.
        $digest = (Get-FileHash -LiteralPath $OwnersFile -Algorithm SHA256).Hash.ToLowerInvariant()
        $configName = "$Stack-owners-$($digest.Substring(0, 16))"
        $known = & docker config ls --filter "name=$configName" --format '{{.Name}}'
        if ($LASTEXITCODE -ne 0) { throw 'Swarm manager access is required' }
        if ($configName -notin @($known)) { Invoke-DockerChecked -DockerArgs @('config', 'create', $configName, $OwnersFile) }
        $previousConfig = $env:KADLAB_OWNERS_CONFIG
        try {
            $env:KADLAB_OWNERS_CONFIG = $configName
            Invoke-DockerChecked -DockerArgs @('stack', 'deploy', '-c', 'docker-compose.yml', $Stack)
        } finally { $env:KADLAB_OWNERS_CONFIG = $previousConfig }
        Write-Host "Stack deployment started: $Stack (1 bootstrap + 49 replicas). Check -Action Status before opening the shell."
    }
    'Shell' {
        if (-not (Test-Path -LiteralPath $PublisherKey)) { throw 'Prepare the publisher key first' }
        $containers = @(& docker ps --filter "label=com.docker.swarm.service.name=${Stack}_kademlia-bootstrap" --format '{{.ID}}')
        if ($LASTEXITCODE -ne 0 -or $containers.Count -ne 1) { throw 'Expected one local running bootstrap container; check -Action Status' }
        $container = $containers[0]
        Invoke-DockerChecked -DockerArgs @('cp', $PublisherKey, "${container}:/tmp/kadlab-publisher.key")
        try {
            Invoke-DockerChecked -DockerArgs @('exec', $container, 'chmod', '600', '/tmp/kadlab-publisher.key')
            & docker exec -it $container /usr/local/bin/kadlab-shell --publisher-key /tmp/kadlab-publisher.key
            $shellExit = $LASTEXITCODE
        } finally {
            # Remove only the temporary copy introduced by this command.
            Invoke-DockerChecked -DockerArgs @('exec', $container, 'rm', '-f', '/tmp/kadlab-publisher.key')
        }
        if ($shellExit -ne 0) { throw 'Interactive CLI failed' }
    }
    'Status' {
        Invoke-DockerChecked -DockerArgs @('stack', 'services', $Stack)
        Invoke-DockerChecked -DockerArgs @('stack', 'ps', '--no-trunc', $Stack)
    }
    'Stop' { Invoke-DockerChecked -DockerArgs @('stack', 'rm', $Stack) }
}
