// TODO: generate these interface type declarations from the go structs instead of doing it by hand.
// See https://github.com/boldsoftware/bold/blob/c6670a0a13f9d25785c8c1a90587fbab20a58bdd/sketch/types/ts.go for an example.

/**
 * Interface for a Git commit
 */
export interface GitCommit {
  hash: string; // Full commit hash
  subject: string; // Commit subject line
  body: string; // Full commit message body
  pushed_branch?: string; // If set, this commit was pushed to this branch
}

/**
 * Interface for a tool call
 */
export interface ToolCall {
  name: string;
  args?: string;
  result?: string;
  input?: string; // Input property for TypeScript compatibility
  tool_call_id?: string;
  result_message?: TimelineMessage;
}

/**
 * Interface for a timeline message
 */
export interface TimelineMessage {
  idx: number;
  type: string;
  content?: string;
  timestamp?: string | number | Date;
  elapsed?: number;
  turnDuration?: number; // Turn duration field
  end_of_turn?: boolean;
  conversation_id?: string;
  parent_conversation_id?: string;
  start_time?: string;
  end_time?: string;
  tool_calls?: ToolCall[];
  tool_name?: string;
  tool_error?: boolean;
  tool_call_id?: string;
  commits?: GitCommit[]; // For commit messages
  input?: string; // Input property
  tool_result?: string; // Tool result property
  toolResponses?: any[]; // Tool responses array
  usage?: Usage;
}

export interface Usage {
  start_time?: string;
  messages?: number;
  input_tokens?: number;
  output_tokens?: number;
  cache_read_input_tokens?: number;
  cache_creation_input_tokens?: number;
  cost_usd?: number;
  total_cost_usd?: number;
  tool_uses?: Map<string, any>;
}
export interface State {
  hostname?: string;
  initial_commit?: string;
  message_count?: number;
  os: string;
  title: string;
  total_usage: Usage; // TODO Make a TotalUseage interface.
  working_dir?: string;
}
