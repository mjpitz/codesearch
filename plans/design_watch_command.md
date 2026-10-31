# Design Document: `watch` Command

## Overview
The `watch` command provides real-time synchronization between the filesystem and the `codesearch` index. It monitors specified directories for changes and automatically triggers a re-indexing process using the existing `sync` logic.

## 1. CLI Interface

The command will be implemented as `codesearch watch [paths...]`.

### Arguments
- `paths...`: A list of directory paths to monitor. If no paths are provided, it defaults to the current working directory.

### Flags
- `-c, --config <path>`: Path to the configuration file. If omitted, the command will attempt to load the default configuration.
- `-i, --ignore <pattern>`: An additional glob pattern to ignore. This is in addition to patterns defined in `.codesearchignore` or `.gitignore`.
- `-d, --debounce <duration>`: The time to wait after the last filesystem event before triggering a re-index (e.g., `500ms`, `2s`). Default is `500ms`.
- `-v, --verbose`: Enables verbose logging for debugging watcher events and sync progress.

## 2. File Watching Strategy

### Library selection
We will use `github.com/fsnotify/fsnotify`, which is the standard Go library for filesystem notifications and provides a consistent interface across platforms.

### Implementation details
- **Recursive Watching**: Since `fsnotify` is not natively recursive on all platforms, the implementation will perform an initial recursive walk of the target directories using `internal/ignore` to identify all subdirectories. Every identified directory will be added to the `fsnotify.Watcher`.
- **Dynamic Directory Management**:
    - When a `Create` event is detected on a directory, the watcher will automatically add it to the active watch list.
    - When a `Remove` event is detected on a directory, the watcher will remove it from the active watch list to prevent errors.
- **Ignore Pattern Integration**: Every event received from the watcher will be passed through the `internal/ignore` matcher. Events belonging to ignored files or directories will be discarded immediately to prevent unnecessary work.

## 3. Re-indexing Workflow

### Event Mapping
The following `fsnotify` events will be captured:
- `Create`: New file or directory created.
- `Write`: File content or metadata modified.
- `Remove`: File or directory deleted.
- `Rename`: File moved or renamed.

### Debouncing Mechanism
To prevent "event storms" (e.g., during a `git checkout` or a bulk file copy), a debouncing mechanism will be implemented:
1. **Event Reception**: When a valid (non-ignored) event occurs, the path is added to a "dirty" state.
2. **Timer Reset**: A `time.Timer` is reset on every valid event.
3. **Trigger**: The re-indexing process is only triggered when the timer successfully expires without being reset by a subsequent event.

### Re-indexing Execution
Once the debounce timer expires, the command will call `sync.Run(ctx, cfg, sync.RunOptions{})`.
- We leverage the existing `sync.Run` because it already implements an efficient reconciliation loop using `mtime` and `size` fast-paths, minimizing the actual work performed during a watch cycle.

## 4. Integration

- **`internal/cmd`**: A new `watch.go` file will be created to house the command definition and CLI logic.
- **`internal/sync`**: The command will serve as a long-running wrapper around `sync.Run`.
- **`internal/ignore`**: Used to filter events by checking path matches against the project's ignore rules.
- **`internal/config`**: Used to retrieve the `IndexConfig` required for synchronization.

## 5. Concurrency and Error Handling

### Concurrency Model
The command will operate using three primary components running in separate goroutines:
1. **Watcher Goroutine**: Listens to the `fsnotify` event channel and handles recursive directory additions/removals.
2. **Debounce/Coordinator Goroutine**: Manages the `time.Timer` and orchestrates the timing of the `sync.Run` calls.
3. **Main Goroutine**: Handles OS signals (SIGINT, SIGTERM) and manages the lifecycle of the application.

### Error Handling
- **Watcher Errors**: Errors from the `fsnotify` channel (e.g., resource exhaustion) will be logged to `stderr`.
- **Sync Errors**: If `sync.Run` returns an error, the error will be logged, and the command will remain active, waiting for the next filesystem event to attempt another sync.
- **Graceful Shutdown**: Upon receiving a termination signal, the `context.Context` passed to all components will be cancelled, ensuring that any ongoing `sync.Run` operations are allowed to complete or exit cleanly and that the watcher is properly closed.
