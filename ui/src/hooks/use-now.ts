import { useEffect, useState } from "react";

export function useNow(intervalMs = 10_000) {
  const [now, setNow] = useState(() => new Date());

  useEffect(() => {
    // Samples the clock at once too, so a faster interval does not start from an old time.
    const first = setTimeout(() => setNow(new Date()), 0);
    const id = setInterval(() => setNow(new Date()), intervalMs);

    return () => {
      clearTimeout(first);
      clearInterval(id);
    };
  }, [intervalMs]);

  return now;
}
