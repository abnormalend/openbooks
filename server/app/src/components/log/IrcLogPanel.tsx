import { Box, Center, Group, Text, useMantineTheme } from "@mantine/core";
import { useEffect, useRef, useState } from "react";
import { useAppSelector } from "../../state/store";

const formatTime = (ts: number): string =>
  new Date(ts).toLocaleTimeString("en-US", { hour12: false });

export default function IrcLogPanel() {
  const theme = useMantineTheme();
  const entries = useAppSelector((state) => state.ircLog.entries);

  const scrollRef = useRef<HTMLDivElement>(null);
  // Stick to bottom only while the user is already pinned there. Once
  // they scroll up to read older entries, new arrivals stop yanking
  // them back down.
  const [stickToBottom, setStickToBottom] = useState(true);

  useEffect(() => {
    if (stickToBottom && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [entries.length, stickToBottom]);

  const onScroll = () => {
    const el = scrollRef.current;
    if (!el) return;
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 4;
    setStickToBottom(atBottom);
  };

  return (
    <Box
      sx={(t) => ({
        height: "100%",
        width: "100%",
        display: "flex",
        flexDirection: "column",
        minHeight: 0,
        borderRadius: t.radius.md,
        border: `1px solid ${
          t.colorScheme === "dark" ? t.colors.dark[5] : t.colors.gray[3]
        }`,
        backgroundColor:
          t.colorScheme === "dark" ? t.colors.dark[8] : t.colors.gray[0]
      })}
    >
      <Group
        position="apart"
        px="sm"
        py={4}
        sx={(t) => ({
          flex: "0 0 auto",
          borderBottom: `1px solid ${
            t.colorScheme === "dark" ? t.colors.dark[5] : t.colors.gray[3]
          }`
        })}
      >
        <Text size="xs" weight={600} color="dimmed" transform="uppercase">
          IRC Log{entries.length > 0 ? ` (${entries.length})` : ""}
        </Text>
        {!stickToBottom && (
          <Text size="xs" color="dimmed">
            paused (scroll to bottom to resume)
          </Text>
        )}
      </Group>
      <div
        ref={scrollRef}
        onScroll={onScroll}
        style={{
          flex: 1,
          overflowY: "auto",
          overflowX: "hidden",
          padding: 6,
          fontFamily: theme.fontFamilyMonospace,
          fontSize: 11,
          minHeight: 0
        }}
      >
        {entries.length === 0 ? (
          <Center sx={{ height: "100%" }}>
            <Text size="xs" color="dimmed">
              Waiting for IRC traffic...
            </Text>
          </Center>
        ) : (
          entries.map((e, i) => (
            <div
              key={i}
              style={{
                whiteSpace: "pre-wrap",
                wordBreak: "break-all",
                lineHeight: 1.5
              }}
            >
              <Text
                component="span"
                color="dimmed"
                sx={{ fontSize: 11, marginRight: 8 }}
              >
                [{formatTime(e.timestamp)}]
              </Text>
              {e.line}
            </div>
          ))
        )}
      </div>
    </Box>
  );
}
