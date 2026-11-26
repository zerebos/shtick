# shtick PowerShell plugin
# Load from $PROFILE, after your aliases and functions are defined:
#   Invoke-Expression (& shtick init pwsh | Out-String)
# or dot-source this file. Records which aliases and functions you use and
# nudges you when you type the long form of one. Set $env:SHTICK_NUDGE = '0'
# to record quietly, or $env:SHTICK_DISABLE = '1' to turn it off.
#
# Needs PSReadLine (loaded by default in pwsh 7 and the Windows PowerShell 5.1
# console). The hook runs from PSReadLine's AddToHistoryHandler, which fires
# after you press Enter and before the command runs.

if ($global:_shtickLoaded) { return }

$global:_shtickExe = Get-Command shtick -CommandType Application -ErrorAction Ignore |
  Select-Object -First 1 -ExpandProperty Source
if (-not $global:_shtickExe) {
  Write-Warning 'shtick: binary not found in PATH'
  return
}

# Same lookup as the binary's store.DefaultPaths; keep them in step.
$global:_shtickState = if ($env:SHTICK_STATE_DIR) { $env:SHTICK_STATE_DIR }
  elseif ($env:XDG_STATE_HOME) { Join-Path $env:XDG_STATE_HOME 'shtick' }
  elseif ($env:OS -eq 'Windows_NT' -and $env:LOCALAPPDATA) { Join-Path $env:LOCALAPPDATA 'shtick\state' }
  else { Join-Path $HOME '.local/state/shtick' }
$global:_shtickDefs = Join-Path (Join-Path $global:_shtickState 'defs') "$PID.defs"
$global:_shtickSig = ''

# Aliases and functions PowerShell defines on its own, so the dump can mark
# them. Aliases count as built-in only while they still point at the default.
# The & { } blocks keep loop variables out of the session.
$global:_shtickBuiltin = @{}
& {
  try {
    foreach ($e in [System.Management.Automation.Runspaces.InitialSessionState]::CreateDefault().Commands) {
      if ($e -is [System.Management.Automation.Runspaces.SessionStateAliasEntry]) {
        $global:_shtickBuiltin["alias:$($e.Name)"] = $e.Definition
      } elseif ($e -is [System.Management.Automation.Runspaces.SessionStateFunctionEntry]) {
        $global:_shtickBuiltin["function:$($e.Name)"] = $true
      }
    }
  } catch {}
}

function global:_shtick_debug($err) {
  if ($env:SHTICK_DEBUG) { Write-Warning "shtick: $err" }
}

function global:_shtick_mkdir([string]$dir) {
  if (Test-Path -LiteralPath $dir) { return }
  New-Item -ItemType Directory -Force -Path $dir | Out-Null
  if ($env:OS -ne 'Windows_NT') { & chmod 700 -- $dir 2>$null }
}

# Write this shell's aliases and functions for the binary to read, NUL
# separated: kind, name, source, value. Only writes when the counts change.
function global:_shtick_dump {
  $als = @(Get-ChildItem -Path alias:)
  $fns = @(Get-ChildItem -Path function:)
  $sig = "$($als.Count) $($fns.Count)"
  if ($sig -eq $global:_shtickSig -and (Test-Path -LiteralPath $global:_shtickDefs)) { return }

  $sb = [System.Text.StringBuilder]::new()
  $nul = [char]0
  $add = {
    param($kind, $name, $source, $value)
    foreach ($field in $kind, $name, $source, $value) {
      [void]$sb.Append([string]$field).Append($nul)
    }
  }
  & $add 'shell' 'pwsh' '' ''
  foreach ($a in $als) {
    $src = if ($global:_shtickBuiltin["alias:$($a.Name)"] -eq $a.Definition) { 'builtin' }
      elseif ($a.Module) { $a.Module.Path }
      else { '' }
    & $add 'alias' $a.Name $src $a.Definition
  }
  foreach ($f in $fns) {
    $src = if ($global:_shtickBuiltin.ContainsKey("function:$($f.Name)")) { 'builtin' }
      elseif ($f.ScriptBlock.File) { $f.ScriptBlock.File }
      elseif ($f.Module) { $f.Module.Path }
      else { '' }
    & $add 'function' $f.Name $src $f.Definition
  }

  _shtick_mkdir (Split-Path -Parent $global:_shtickDefs)
  $tmp = "$global:_shtickDefs.tmp"
  [System.IO.File]::WriteAllText($tmp, $sb.ToString(), [System.Text.UTF8Encoding]::new($false))
  Move-Item -LiteralPath $tmp -Destination $global:_shtickDefs -Force
  $global:_shtickSig = $sig
}

function global:_shtick_histfile {
  try { (Get-PSReadLineOption).HistorySavePath } catch { $null }
}

# Called with each accepted line, before it runs.
function global:_shtick_hook([string]$line) {
  if ($env:SHTICK_DISABLE -or [string]::IsNullOrWhiteSpace($line)) { return }
  # running the binary would overwrite the exit code the user's next
  # command may want to read
  $exit = $global:LASTEXITCODE
  $enc = $null
  try {
    _shtick_dump
    $env:SHTICK_DEFS = $global:_shtickDefs
    $env:SHTICK_LINE = $line
    # read the binary's output as UTF-8; hosts without a console refuse this
    try {
      $enc = [Console]::OutputEncoding
      [Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)
    } catch {}
    $out = & $global:_shtickExe hook --stdout
    foreach ($l in @($out)) { if ($l) { Write-Host $l } }
  } catch {
    _shtick_debug $_
  } finally {
    if ($enc) { try { [Console]::OutputEncoding = $enc } catch {} }
    Remove-Item -Path env:SHTICK_LINE, env:SHTICK_DEFS -ErrorAction Ignore
    $global:LASTEXITCODE = $exit
  }
}

# Reports go through this wrapper so they see this shell's definitions and
# history file.
function global:shtick {
  $global:_shtickSig = ''
  try { _shtick_dump } catch { _shtick_debug $_ }
  $env:SHTICK_DEFS = $global:_shtickDefs
  $env:SHTICK_SHELL = 'pwsh'
  $hist = _shtick_histfile
  if ($hist) { $env:SHTICK_HISTFILE = $hist }
  try {
    & $global:_shtickExe @args
  } finally {
    Remove-Item -Path env:SHTICK_DEFS, env:SHTICK_SHELL, env:SHTICK_HISTFILE -ErrorAction Ignore
  }
}

# PSReadLine history has no timestamps, so remember how long it was when
# shtick first loaded. shtick import reads only what came before.
& {
  try {
    $hist = _shtick_histfile
    $mark = Join-Path $global:_shtickState 'pwsh-history.mark'
    if ($hist -and -not (Test-Path -LiteralPath $mark)) {
      $len = if (Test-Path -LiteralPath $hist) { (Get-Item -LiteralPath $hist).Length } else { 0 }
      _shtick_mkdir $global:_shtickState
      [System.IO.File]::WriteAllText($mark, "$len")
    }
  } catch { _shtick_debug $_ }
}

# Profiles can run before PSReadLine is imported; asking for its options
# loads it.
$global:_shtickPsrl = try { Get-PSReadLineOption -ErrorAction Stop } catch { $null }
if ($global:_shtickPsrl) {
  # Keep whatever handler was there (PSReadLine's default filters secrets out
  # of the history file) and return its answer.
  $global:_shtickPrevHandler = $global:_shtickPsrl.AddToHistoryHandler
  Set-PSReadLineOption -AddToHistoryHandler {
    param([string]$line)
    _shtick_hook $line
    $prev = $global:_shtickPrevHandler
    if ($prev -is [scriptblock]) { return & $prev $line }
    if ($prev) { return $prev.Invoke($line) }
    return $true
  }
} else {
  Write-Warning 'shtick: PSReadLine is not loaded, so commands will not be recorded'
}

$null = Register-EngineEvent -SourceIdentifier PowerShell.Exiting -Action {
  Remove-Item -LiteralPath $global:_shtickDefs -Force -ErrorAction Ignore
}

$global:_shtickLoaded = $true
