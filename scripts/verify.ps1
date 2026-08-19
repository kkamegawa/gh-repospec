[CmdletBinding()]
param(
    [switch]$Race,
    [switch]$Lint
)

$ErrorActionPreference = 'Stop'
$packages = @('./...')

function Invoke-Go {
    param([Parameter(ValueFromRemainingArguments = $true)][string[]]$Arguments)

    & go @Arguments
    if ($LASTEXITCODE -ne 0) {
        exit $LASTEXITCODE
    }
}

$testArguments = @('test', '-shuffle=on') + $packages
Invoke-Go $testArguments

$vetArguments = @('vet') + $packages
Invoke-Go $vetArguments

New-Item -ItemType Directory -Path bin -Force | Out-Null
$buildArguments = @('build', '-o', 'bin/gh-repobootstrapper.exe', './cmd/gh-repobootstrapper')
Invoke-Go $buildArguments

if ($Race) {
    $raceArguments = @('test', '-race', '-shuffle=on', '-count=1') + $packages
    Invoke-Go $raceArguments
}

if ($Lint) {
    & golangci-lint run @packages
    if ($LASTEXITCODE -ne 0) {
        exit $LASTEXITCODE
    }
}
