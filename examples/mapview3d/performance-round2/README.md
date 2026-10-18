# Combined local pre-release performance round

2026-10-18, Apple M5 Max, darwin/arm64, Go 1.27.2; WASM runs under Node.
Production pins and sibling working trees were not changed. No release was made.

## Tested source and method

Released baseline: nt3d v0.2.0, ntgpuimage v0.3.0, ntcharts3d v0.3.1.
Combined local snapshots:

- nt3d `6cd83d5acb93a0337d1693960a719fab0bb2d942` plus captured uncommitted
  point-update changes, preserved in [nt3d-snapshot-extra.patch](nt3d-snapshot-extra.patch).
- ntgpuimage `34fa60e141edb03d18ab6f297ea4a6843b6c8a0c`.
- ntcharts3d `f5ee68cb05686221c8b6056163606c42819c6ba0`.

The ntcharts3d revision adds measurement code, not a production renderer-lifetime
optimization. Thus the measured production gains come from the combined nt3d and
ntgpuimage changes. This round does not independently attribute gains to each.

Snapshots of working files were copied into `/tmp/osm-perf-round2`; workspaces
select released or local dependencies without editing project modules. Untracked
gallery benchmark artifacts were excluded. [revisions.json](revisions.json)
records Git state; [source-sha256.json](source-sha256.json) identifies copied Go
sources, including the uncommitted consumer. Results apply to these snapshots,
not subsequent edits in another agent's working tree.

All samples ran sequentially after tests/builds. Each table reports the median
of three samples; raw output includes individual times and allocation figures.
Snapshot cases use exactly 144 operations (two full camera rotations).
Native presentation glyph/direct cases use 72 operations; shm cases use separate
processes of 20 operations to avoid accumulating unconsumed objects in the sink.
WASM glyph uses 20 operations. These are local timings, not statistical confidence
intervals or latency percentiles. Small differences in the bars control are noise
at this measurement resolution, not evidence of a meaningful regression.

## Validation

- Full root-module race tests and vet passed for all three combined libraries,
  the mapview library, and its examples module. Nested independent gallery and
  transport modules were not part of this test matrix.
- 216 software snapshots matched the released baseline byte for byte: one full
  72-frame rotation at each anisotropy setting 1, 4, and 16, at 640×320.
  See [pixels-baseline.txt](pixels-baseline.txt), [pixels-local.txt](pixels-local.txt),
  and [pixel_oracle_test.go.txt](pixel_oracle_test.go.txt).
- Both local WASM demos built and passed Node lifecycle tests: startup, action
  publication, resize, input, quit. Tile fetch failures in the log are deliberate
  offline fixtures, not observed network failures.
- Native presentation checks retained software mode, 4× anisotropy and zero
  adaptive shrink. Shared-memory samples reported shm frames, with no direct
  fallback. Bars and burst/coalescing cases passed too.
- WASM presentation lifecycle and glyph timing checks passed for both variants.
- Full-resolution direct WASM timing was rejected on the **released baseline**
  because adaptive quality changed. There is no valid paired 640×320 direct-WASM
  timing claim in this report; the local direct timing was not attempted after
  that baseline failure. Preserve [wasm-presentation-baseline.txt](wasm-presentation-baseline.txt)
  as evidence of this measurement gap. It is not a new-code regression.

GPU-enabled parity, real-terminal consumption/painting, actual browser visual
performance, TinyGo, and the owning repositories' complete release matrices were
not established here. Native shared-memory sink completion does not prove that a
terminal consumed the images. See [validation.txt](validation.txt) and
[browser.txt](browser.txt) for executed checks.

## Snapshot measurements

Fixed resolution, software renderer, no tile fetch/composition, image encoding,
terminal output, or adaptive downscaling. Camera frames reuse a warmed texture;
replacement includes a fresh texture and its mipmaps.

| Workload | Released ms/op | Local ms/op | Time reduction |
| --- | ---: | ---: | ---: |
| Camera/320x160/aniso1 | 1.673 | 1.101 | 34.2% |
| Camera/320x160/aniso4 | 2.295 | 1.523 | 33.6% |
| Camera/640x320/aniso1 | 6.578 | 4.317 | 34.4% |
| Camera/640x320/aniso4 | 8.919 | 6.132 | 31.2% |
| Camera/1280x640/aniso1 | 25.729 | 17.408 | 32.3% |
| Camera/1280x640/aniso4 | 27.126 | 18.570 | 31.5% |
| TextureReplacement | 14.777 | 7.452 | 49.6% |
| NewTexture | 0.211 | 0.195 | 7.4% |

[Released raw data](snapshot-baseline.txt) · [Local raw data](snapshot-local.txt).
Camera allocations remain 8/op (about 1.56 MiB at 640×320); texture replacement
remains 39/op (about 3.90 MiB). This round improves computation, not framebuffer
allocation. Depth-scratch reuse remains future coordinated work.

## Native presentation measurements

The same new ntcharts3d harness runs with released dependencies for baseline and
all local dependencies for comparison. It runs actual chart commands/Update,
ntpicture encoding and ordered RawMsg delivery into a sink, waits for PresentedMsg,
and constructs chart View. It excludes terminal painting and Bubble Tea screen
diffing. Glyph renders 80×40 pixels; direct/shm render 640×320. Do not compare
transport rows as though glyph had the same pixel workload. Texture cases hold
the camera fixed; snapshot replacement rotates, so their timings are not directly
interchangeable. Burst includes four requests and two completed frames per op.

| Workload | Released ms/op | Local ms/op | Time reduction |
| --- | ---: | ---: | ---: |
| glyph/camera | 0.253 | 0.229 | 9.4% |
| glyph/texture | 6.042 | 1.725 | 71.5% |
| glyph/bars | 0.147 | 0.146 | 0.3% |
| glyph/burst | 0.441 | 0.347 | 21.4% |
| direct/camera | 10.020 | 6.972 | 30.4% |
| direct/texture | 15.626 | 8.246 | 47.2% |
| direct/bars | 2.823 | 2.853 | -1.1% |
| direct/burst | 22.486 | 16.308 | 27.5% |
| shm/camera | 9.345 | 6.518 | 30.3% |
| shm/texture | 15.306 | 8.030 | 47.5% |
| shm/bars | 2.657 | 2.659 | -0.1% |
| shm/burst | 21.197 | 15.198 | 28.3% |

[Released raw data](presentation-baseline.txt) · [Local raw data](presentation-local.txt).
Inspect output/render stage timings, counts, protocol bytes, actual transport and
raster dimensions in these files. No quality settings were reduced for speed.

## WASM glyph presentation

Node application-side timing, not browser emulator painting. Raster is 80×40.

| Workload | Released ms/op | Local ms/op | Time reduction |
| --- | ---: | ---: | ---: |
| glyph/camera | 0.764 | 0.616 | 19.3% |
| glyph/texture | 33.301 | 5.563 | 83.3% |

[Released raw data](wasm-glyph-baseline.txt) · [Local raw data](wasm-glyph-local.txt).

## Release assessment and next steps

No correctness blocker surfaced in the tested map path. Native camera costs fell
about 31%; replacement+render nearly halved. The presentation harness confirms
that these CPU gains carry through encoding/output completion. Exact map pixels
were preserved in the checked views and filtering modes.

Before tagging, settle/include or exclude the captured nt3d point-update work
explicitly and run the owners' remaining release gates for the final commits.
Release ntgpuimage, update nt3d's dependency to that release, then release nt3d
and update/release ntcharts3d as appropriate. ntcharts3d currently has only new
measurement code, but its dependency pin must select the optimized nt3d for
ordinary downstream upgrades. This repo can then update its published pins and
regenerate notices. No local replacements should be shipped.

The clearest remaining measurement gap is full-size direct Kitty presentation
under WASM. Test adaptive behavior in a real browser or add a fixed-resolution
benchmark deliberately; do not accept a downscaled sample as an apples-to-apples
speedup. Framebuffer allocation is still unchanged.

## Reproduction artifacts

`validate.py`, `browser.py`, `bench.py`, and `wasm-bench.py` record the commands
used. They expect the disposable snapshots/workspaces at `/tmp/osm-perf-round2`;
they do not reconstruct them. Recreate the listed revisions plus the captured
patch and matching consumer source before rerunning after that directory is
removed. Scripts require Go/Node and normal cache/native shm permissions.
