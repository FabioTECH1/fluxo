// xterm parses writes asynchronously. Bound bytes awaiting its callbacks, even
// when the network can deliver output faster than the browser can render it.
export function terminalOutput(
  write: (data: Uint8Array, complete: () => void) => void,
  overflow: () => void,
  limit = 256 * 1024,
  rendered: (bytes: number) => void = () => {},
) {
  let pending = 0;
  let closed = false;
  return {
    write(data: Uint8Array) {
      if (closed) return;
      if (data.byteLength > limit - pending) {
        closed = true;
        overflow();
        return;
      }
      pending += data.byteLength;
      let completed = false;
      write(data, () => {
        if (completed) return;
        completed = true;
        pending -= data.byteLength;
        if (!closed) rendered(data.byteLength);
      });
    },
    close() { closed = true; },
  };
}
