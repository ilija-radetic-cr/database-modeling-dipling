const key = (projectId: string) => `dbdsl:trace-focus:${projectId}`;

export function requestTraceFocus(projectId: string, elementId: string) {
  try {
    sessionStorage.setItem(key(projectId), elementId);
  } catch {
    // The trace page still opens even when browser storage is unavailable.
  }
}

export function consumeTraceFocus(projectId: string) {
  try {
    const elementId = sessionStorage.getItem(key(projectId));
    if (elementId) {
      // Defer removal so React StrictMode's second development render sees the
      // same one-shot focus request.
      setTimeout(() => {
        try {
          sessionStorage.removeItem(key(projectId));
        } catch {
          // Nothing to clean up when browser storage is unavailable.
        }
      }, 0);
    }
    return elementId;
  } catch {
    return null;
  }
}
