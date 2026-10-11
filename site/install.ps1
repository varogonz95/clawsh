# Installs clawsh on Windows from its GitHub release:
#
#   irm https://varogonz95.github.io/clawsh/install.ps1 | iex
#
# Downloads the archive for this machine, checks it against the release's
# checksums.txt, puts clawsh.exe in %LOCALAPPDATA%\Programs\clawsh and adds
# that folder to your user PATH (no admin). Settings, set before running:
#   $env:CLAWSH_VERSION = '0.1.0'         a specific release instead of the latest
#   $env:CLAWSH_INSTALL_DIR = 'C:\tools'  where clawsh.exe goes
#
# Everything is one script block, so a download cut off halfway fails to
# parse instead of running half a script, and nothing leaks into your session.

& {
	$ErrorActionPreference = 'Stop'
	$ProgressPreference = 'SilentlyContinue' # the progress bar makes downloads crawl on PowerShell 5.1
	[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

	$releases = if ($env:CLAWSH_RELEASES_URL) { $env:CLAWSH_RELEASES_URL.TrimEnd('/') } else { 'https://github.com/varogonz95/clawsh/releases' }
	function Say($msg) { Write-Host "clawsh: $msg" }
	function Fail($msg) { throw "clawsh: $msg" }

	# The machine's architecture, not this PowerShell's: x64 PowerShell also
	# runs (emulated) on ARM64 Windows.
	$arch = $null
	try { $arch = (Get-ItemProperty 'HKLM:\SYSTEM\CurrentControlSet\Control\Session Manager\Environment').PROCESSOR_ARCHITECTURE } catch {}
	if (-not $arch) { $arch = $env:PROCESSOR_ARCHITECTURE }
	switch ($arch) {
		'AMD64' { $goarch = 'amd64' }
		'ARM64' { $goarch = 'arm64' }
		default { Fail "no prebuilt binary for '$arch'; build from source: https://github.com/varogonz95/clawsh" }
	}

	if ($env:CLAWSH_VERSION) {
		$tag = 'v' + $env:CLAWSH_VERSION.TrimStart('v')
	} else {
		# releases/latest redirects to releases/tag/<tag>.
		$r = Invoke-WebRequest -Uri "$releases/latest" -Method Head -UseBasicParsing
		$final = if ($r.BaseResponse.ResponseUri) { $r.BaseResponse.ResponseUri.AbsoluteUri } else { $r.BaseResponse.RequestMessage.RequestUri.AbsoluteUri }
		$tag = $final.TrimEnd('/').Split('/')[-1]
		if ($tag -notmatch '^v\d') { Fail "couldn't find the latest release at $releases/latest" }
	}
	$version = $tag.Substring(1)
	$name = "clawsh_${version}_windows_${goarch}"
	$dir = if ($env:CLAWSH_INSTALL_DIR) { $env:CLAWSH_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'Programs\clawsh' }

	$tmp = Join-Path ([IO.Path]::GetTempPath()) ("clawsh-" + [guid]::NewGuid())
	New-Item -ItemType Directory -Path $tmp | Out-Null
	try {
		Say "downloading clawsh $version for windows/$goarch"
		$zip = Join-Path $tmp "$name.zip"
		$sums = Join-Path $tmp 'checksums.txt'
		try {
			Invoke-WebRequest -Uri "$releases/download/$tag/$name.zip" -OutFile $zip -UseBasicParsing
			Invoke-WebRequest -Uri "$releases/download/$tag/checksums.txt" -OutFile $sums -UseBasicParsing
		} catch {
			Fail "couldn't download clawsh $version from $releases/download/${tag}: $($_.Exception.Message)"
		}

		$want = $null
		foreach ($line in Get-Content $sums) {
			$f = $line -split '\s+', 2
			if ($f.Count -eq 2 -and $f[1].TrimStart('*') -eq "$name.zip") { $want = $f[0].ToLower() }
		}
		if (-not $want) { Fail "$name.zip isn't listed in the release's checksums.txt" }
		if ((Get-FileHash -Algorithm SHA256 $zip).Hash.ToLower() -ne $want) { Fail "$name.zip failed its SHA-256 check; not installing" }

		Expand-Archive -Path $zip -DestinationPath $tmp -Force
		$exe = Join-Path (Join-Path $tmp $name) 'clawsh.exe'
		if (-not (Test-Path $exe)) { Fail "the archive has no $name\clawsh.exe" }

		New-Item -ItemType Directory -Path $dir -Force | Out-Null
		$target = Join-Path $dir 'clawsh.exe'
		# A running clawsh.exe can't be overwritten, but it can be renamed.
		if (Test-Path $target) {
			Move-Item -Path $target -Destination "$target.old" -Force
		}
		Copy-Item -Path $exe -Destination $target
		Remove-Item "$target.old" -Force -ErrorAction SilentlyContinue
		$installed = try { & $target version } catch { "clawsh $version" }
		Say "installed $installed to $target"
	} finally {
		Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
	}

	$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
	$parts = if ($userPath) { $userPath.Split(';') } else { @() }
	if ($parts -notcontains $dir) {
		[Environment]::SetEnvironmentVariable('Path', (@($parts | Where-Object { $_ }) + $dir) -join ';', 'User')
		$env:Path = "$env:Path;$dir"
		Say "added $dir to your user PATH; open a new terminal to use clawsh there"
	}
}
