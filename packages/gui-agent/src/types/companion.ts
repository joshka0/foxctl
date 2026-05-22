export interface PresenceBundle {
  emotion?: string;
  intensity?: number;
  display_text?: string;
  markers?: string[];
  detected_emoji?: string[];
  background_digest?: string;
  overlay_digest?: string;
  audio_digest?: string;
  audio_duration_ms?: number;
  cache_hits?: number;
  cache_misses?: number;
  errors?: string[];
}

export interface CompanionConversation {
  id: string;
  title?: string;
  created_at: string;
  updated_at: string;
  message_count: number;
}
