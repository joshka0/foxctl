<script lang="ts">
  import {
    RefreshCw,
    Send,
    Plus,
    Trash2,
    Terminal,
    MessageSquare,
    Wrench,
    Bot,
    User,
    XCircle,
    Loader2,
    ChevronRight,
  } from "lucide-svelte";
  import Layout from "@/lib/components/layout/Layout.svelte";
  import { createQuery } from "@tanstack/svelte-query";
  import {
    Badge,
    Button,
    Card,
    CardContent,
    Input,
  } from "@/lib/components/ui";
  import {
    askConsoleSession,
    cancelConsoleSession,
    createConsoleSession,
    deleteConsoleSession,
    listConsoleSessions,
    subscribeToConsoleSessionEvents,
    type ConsolePayload,
    type ConsoleSessionInfo,
  } from "@/lib/api/console";
  import { onDestroy } from "svelte";

  // State
  let selectedConsoleId = "";
  let messageInput = "";
  let isCreating = false;
  let isSending = false;
  let currentAskId = "";

  // Message types for the transcript
  interface TranscriptMessage {
    id: string;
    type: "user" | "assistant" | "tool_call" | "tool_result" | "thought" | "progress";
    content: string;
    timestamp: Date;
    toolName?: string;
    toolInput?: string;
    casDigest?: string;
    isStreaming?: boolean;
  }

  let transcript: TranscriptMessage[] = [];
  let unsubscribe: (() => void) | null = null;

  // Queries
  const consolesQuery = createQuery({
    queryKey: ["console-sessions"],
    queryFn: () => listConsoleSessions(),
  });

  $: consoles = $consolesQuery.data?.sessions || [];

  // Subscribe to SSE events when a console is selected
  $: if (selectedConsoleId && typeof window !== "undefined") {
    setupEventSubscription(selectedConsoleId);
  }

  function setupEventSubscription(consoleId: string) {
    // Clean up previous subscription
    if (unsubscribe) {
      unsubscribe();
      unsubscribe = null;
    }

    unsubscribe = subscribeToConsoleSessionEvents(
      consoleId,
      handleConsolePayload,
      handleSSEError
    );
  }

  function handleConsolePayload(payload: ConsolePayload) {
    const correlationId = payload.correlation_id || currentAskId || "unknown";

    if (payload.type === "event") {
      const metadata = payload.metadata || {};
      const phase = typeof metadata.phase === "string" ? metadata.phase : "";
      const isPartial = metadata.partial === true;

      if (isPartial && payload.content) {
        updateStreamingMessage(correlationId, payload.content);
        return;
      }

      const toolName = typeof metadata.tool === "string" ? metadata.tool : undefined;
      const toolInput =
        metadata.arguments === undefined
          ? undefined
          : typeof metadata.arguments === "string"
            ? metadata.arguments
            : JSON.stringify(metadata.arguments, null, 2);

      if (phase === "call") {
        addTranscriptMessage({
          id: `${correlationId}-tool-call-${Date.now()}`,
          type: "tool_call",
          content: payload.content || "",
          timestamp: new Date(),
          toolName,
          toolInput,
        });
        return;
      }

      if (phase === "result") {
        addTranscriptMessage({
          id: `${correlationId}-tool-result-${Date.now()}`,
          type: "tool_result",
          content: payload.content || "",
          timestamp: new Date(),
          toolName,
          toolInput,
        });
        return;
      }

      addTranscriptMessage({
        id: `${correlationId}-event-${Date.now()}`,
        type: "progress",
        content: payload.content || "",
        timestamp: new Date(),
      });
      return;
    }

    if (payload.type === "reply") {
      transcript = transcript.map((msg) =>
        msg.isStreaming ? { ...msg, isStreaming: false } : msg
      );

      addTranscriptMessage({
        id: `reply-${correlationId}`,
        type: "assistant",
        content: payload.content || "",
        timestamp: new Date(),
      });

      currentAskId = "";
      isSending = false;
    }
  }

  function handleSSEError(error: Event) {
    console.error("Console SSE error:", error);
  }

  function addTranscriptMessage(msg: TranscriptMessage) {
    transcript = [...transcript, msg];
    // Auto-scroll to bottom
    setTimeout(() => {
      const container = document.getElementById("transcript-container");
      if (container) {
        container.scrollTop = container.scrollHeight;
      }
    }, 10);
  }

  function updateStreamingMessage(askId: string, delta: string) {
    const existingIndex = transcript.findIndex(
      (msg) => msg.id === `streaming-${askId}`
    );

    if (existingIndex >= 0) {
      transcript[existingIndex].content += delta;
      transcript = [...transcript];
    } else {
      addTranscriptMessage({
        id: `streaming-${askId}`,
        type: "assistant",
        content: delta,
        timestamp: new Date(),
        isStreaming: true,
      });
    }
  }

  async function handleCreateConsole() {
    isCreating = true;
    try {
      const response = await createConsoleSession({
        profile: "explorer",
      });
      await $consolesQuery.refetch();
      selectedConsoleId = response.session.id;
      transcript = [];
    } catch (err) {
      console.error("Failed to create console:", err);
    } finally {
      isCreating = false;
    }
  }

  async function handleDeleteConsole(id: string) {
    if (!confirm("Delete this console session?")) return;

    try {
      await deleteConsoleSession(id);
      if (selectedConsoleId === id) {
        selectedConsoleId = "";
        transcript = [];
      }
      await $consolesQuery.refetch();
    } catch (err) {
      console.error("Failed to delete console:", err);
    }
  }

  async function handleSendMessage() {
    if (!messageInput.trim() || !selectedConsoleId || isSending) return;

    const content = messageInput.trim();
    messageInput = "";
    isSending = true;

    // Add user message to transcript immediately
    addTranscriptMessage({
      id: `user-${Date.now()}`,
      type: "user",
      content,
      timestamp: new Date(),
    });

    try {
      const response = await askConsoleSession(selectedConsoleId, content);
      currentAskId = response.correlation_id;
    } catch (err) {
      console.error("Failed to send message:", err);
      addTranscriptMessage({
        id: `error-${Date.now()}`,
        type: "assistant",
        content: `Error: ${err instanceof Error ? err.message : "Failed to send message"}`,
        timestamp: new Date(),
      });
      isSending = false;
    }
  }

  async function handleCancel() {
    if (!selectedConsoleId || !currentAskId) return;

    try {
      await cancelConsoleSession(selectedConsoleId, currentAskId);
      isSending = false;
      currentAskId = "";
    } catch (err) {
      console.error("Failed to cancel request:", err);
    }
  }

  function handleKeyDown(event: Event) {
    const keyboardEvent = event as KeyboardEvent;
    if (keyboardEvent.key === "Enter" && !keyboardEvent.shiftKey) {
      keyboardEvent.preventDefault();
      handleSendMessage();
    }
  }

  function selectConsole(id: string) {
    selectedConsoleId = id;
    transcript = []; // Clear transcript when switching
  }

  function formatTime(date: Date): string {
    return date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  }

  function truncateId(id: string): string {
    return id.slice(0, 8);
  }

  function getSessionStatus(session: ConsoleSessionInfo): "active" | "idle" {
    return session.client_count > 0 ? "active" : "idle";
  }

  function getStatusColor(status: string): "default" | "success" | "warning" | "destructive" {
    switch (status) {
      case "active":
        return "success";
      case "idle":
        return "default";
      case "error":
        return "destructive";
      default:
        return "warning";
    }
  }

  onDestroy(() => {
    if (unsubscribe) {
      unsubscribe();
    }
  });
</script>

<Layout>
  <div class="h-[calc(100vh-4rem)] flex">
    <!-- Sessions sidebar -->
    <div class="w-64 border-r flex flex-col bg-muted/30">
      <div class="p-4 border-b">
        <div class="flex items-center justify-between mb-3">
          <h2 class="font-semibold text-sm">Sessions</h2>
          <Button
            variant="ghost"
            size="sm"
            on:click={() => $consolesQuery.refetch()}
            disabled={$consolesQuery.isFetching}
          >
            <RefreshCw class="h-4 w-4 {$consolesQuery.isFetching ? 'animate-spin' : ''}" />
          </Button>
        </div>
        <Button
          variant="outline"
          size="sm"
          class="w-full"
          on:click={handleCreateConsole}
          disabled={isCreating}
        >
          {#if isCreating}
            <Loader2 class="h-4 w-4 mr-2 animate-spin" />
          {:else}
            <Plus class="h-4 w-4 mr-2" />
          {/if}
          New Session
        </Button>
      </div>

      <div class="flex-1 overflow-y-auto p-2 space-y-1">
        {#if $consolesQuery.isLoading}
          <div class="flex items-center justify-center py-8">
            <RefreshCw class="h-5 w-5 animate-spin text-muted-foreground" />
          </div>
        {:else if consoles.length === 0}
          <div class="text-center py-8 text-muted-foreground text-sm">
            No sessions yet
          </div>
        {:else}
          {#each consoles as session (session.id)}
            {@const status = getSessionStatus(session)}
            <button
              type="button"
              class="w-full p-2 rounded-md text-left hover:bg-muted transition-colors group {selectedConsoleId === session.id ? 'bg-muted' : ''}"
              on:click={() => selectConsole(session.id)}
            >
              <div class="flex items-center justify-between">
                <div class="flex items-center gap-2 min-w-0">
                  <Terminal class="h-4 w-4 shrink-0 text-muted-foreground" />
                  <span class="text-sm font-mono truncate">
                    {truncateId(session.id)}
                  </span>
                </div>
                <button
                  type="button"
                  class="opacity-0 group-hover:opacity-100 p-1 hover:bg-destructive/10 rounded"
                  on:click|stopPropagation={() => handleDeleteConsole(session.id)}
                >
                  <Trash2 class="h-3 w-3 text-destructive" />
                </button>
              </div>
              <div class="flex items-center gap-2 mt-1">
                <Badge variant={getStatusColor(status)} class="text-xs">
                  {status}
                </Badge>
                {#if session.message_count}
                  <span class="text-xs text-muted-foreground">
                    {session.message_count} messages
                  </span>
                {/if}
              </div>
            </button>
          {/each}
        {/if}
      </div>
    </div>

    <!-- Main content area -->
    <div class="flex-1 flex flex-col">
      {#if !selectedConsoleId}
        <!-- Empty state -->
        <div class="flex-1 flex items-center justify-center">
          <div class="text-center">
            <Terminal class="h-12 w-12 mx-auto mb-4 text-muted-foreground" />
            <h2 class="text-lg font-semibold mb-2">Console</h2>
            <p class="text-muted-foreground mb-4">
              Select a session or create a new one to start chatting
            </p>
            <Button on:click={handleCreateConsole} disabled={isCreating}>
              {#if isCreating}
                <Loader2 class="h-4 w-4 mr-2 animate-spin" />
              {:else}
                <Plus class="h-4 w-4 mr-2" />
              {/if}
              New Session
            </Button>
          </div>
        </div>
      {:else}
        <!-- Transcript area -->
        <div
          id="transcript-container"
          class="flex-1 overflow-y-auto p-4 space-y-4"
        >
          {#if transcript.length === 0}
            <div class="flex items-center justify-center h-full text-muted-foreground">
              <div class="text-center">
                <MessageSquare class="h-8 w-8 mx-auto mb-2" />
                <p class="text-sm">Start a conversation</p>
              </div>
            </div>
          {:else}
            {#each transcript as message (message.id)}
              <div class="flex gap-3 {message.type === 'user' ? 'justify-end' : ''}">
                {#if message.type !== 'user'}
                  <div class="shrink-0 w-8 h-8 rounded-full bg-muted flex items-center justify-center">
                    {#if message.type === 'assistant'}
                      <Bot class="h-4 w-4" />
                    {:else if message.type === 'tool_call'}
                      <Wrench class="h-4 w-4 text-blue-500" />
                    {:else if message.type === 'tool_result'}
                      <ChevronRight class="h-4 w-4 text-green-500" />
                    {:else}
                      <MessageSquare class="h-4 w-4 text-muted-foreground" />
                    {/if}
                  </div>
                {/if}

                <div class="max-w-[80%] {message.type === 'user' ? 'order-first' : ''}">
                  <div class="flex items-center gap-2 mb-1">
                    <span class="text-xs font-medium capitalize">
                      {message.type === 'tool_call' ? 'Tool Call' :
                       message.type === 'tool_result' ? 'Tool Result' : message.type}
                    </span>
                    {#if message.toolName}
                      <Badge variant="outline" class="text-xs">{message.toolName}</Badge>
                    {/if}
                    <span class="text-xs text-muted-foreground">
                      {formatTime(message.timestamp)}
                    </span>
                    {#if message.isStreaming}
                      <Loader2 class="h-3 w-3 animate-spin text-muted-foreground" />
                    {/if}
                  </div>

                  <Card class="{message.type === 'user' ? 'bg-primary text-primary-foreground' :
                              message.type === 'tool_call' ? 'bg-blue-500/10 border-blue-500/30' :
                              message.type === 'tool_result' ? 'bg-green-500/10 border-green-500/30' : ''}">
                    <CardContent class="p-3">
                      {#if message.type === 'tool_call' && message.toolInput}
                        <pre class="text-xs font-mono whitespace-pre-wrap break-all">{message.toolInput}</pre>
                      {:else}
                        <div class="text-sm whitespace-pre-wrap">{message.content}</div>
                      {/if}

                      {#if message.casDigest}
                        <div class="mt-2 pt-2 border-t border-muted">
                          <span class="text-xs text-muted-foreground font-mono">
                            CAS: {message.casDigest.slice(0, 16)}...
                          </span>
                        </div>
                      {/if}
                    </CardContent>
                  </Card>
                </div>

                {#if message.type === 'user'}
                  <div class="shrink-0 w-8 h-8 rounded-full bg-primary flex items-center justify-center">
                    <User class="h-4 w-4 text-primary-foreground" />
                  </div>
                {/if}
              </div>
            {/each}
          {/if}
        </div>

        <!-- Input area -->
        <div class="border-t p-4 bg-background">
          <div class="flex gap-2">
            <div class="flex-1">
              <Input
                placeholder="Type a message..."
                bind:value={messageInput}
                on:keydown={handleKeyDown}
                disabled={isSending}
                class="h-11"
              />
            </div>
            {#if isSending}
              <Button
                variant="destructive"
                size="icon"
                class="h-11 w-11"
                on:click={handleCancel}
              >
                <XCircle class="h-5 w-5" />
              </Button>
            {:else}
              <Button
                size="icon"
                class="h-11 w-11"
                on:click={handleSendMessage}
                disabled={!messageInput.trim()}
              >
                <Send class="h-5 w-5" />
              </Button>
            {/if}
          </div>
          {#if currentAskId}
            <div class="mt-2 text-xs text-muted-foreground">
              In-flight: {truncateId(currentAskId)}...
            </div>
          {/if}
        </div>
      {/if}
    </div>
  </div>
</Layout>
