export interface ContextWikiProposalWorkPacket {
  proposal_id: string;
  proposal_kind: string;
  action: string;
  status: string;
  review_required: boolean;
  draft_path?: string;
  target_path?: string;
  heading?: string;
  policy_path?: string;
  promotion_job_id?: string;
  requires_vault_path?: boolean;
  vault_path?: string;
  next_command?: string;
}

export interface ContextWikiMaintenanceTask {
  id: string;
  title: string;
  kind: string;
  priority: number;
  reason: string;
  source_refs?: string[];
  work_packet?: ContextWikiProposalWorkPacket | null;
  status: string;
  created_at: string;
}

export interface ContextWikiNextProposalMergeResult {
  workspace_path: string;
  vault_path?: string;
  found: boolean;
  task?: ContextWikiMaintenanceTask | null;
  work_packet?: ContextWikiProposalWorkPacket | null;
  claimed?: boolean;
}

export interface ContextWikiEvidenceImportRun {
  id: string;
  source_kind: string;
  source_ref: string;
  title: string;
  draft_path: string;
  artifact_digest?: string;
  processor_kind?: string;
  processor_model?: string;
  summary: string;
  status: string;
  created_at: string;
}

export interface ContextWikiPromotionJob {
  id: string;
  source_ref: string;
  source_kind: string;
  note_type: string;
  title: string;
  draft_path: string;
  status: string;
  created_at: string;
}

export interface ContextWikiOverviewStats {
  proposal_count: number;
  active_proposal_count: number;
  prepared_merge_count: number;
  claimed_merge_count: number;
  evidence_import_count: number;
  promotion_draft_count: number;
  promotion_merged_count: number;
}

export interface ContextWikiOverview {
  workspace_path: string;
  vault_path?: string;
  stats: ContextWikiOverviewStats;
  next_proposal_merge?: ContextWikiMaintenanceTask | null;
  claimed_proposal_merge?: ContextWikiMaintenanceTask | null;
  proposals: ContextWikiMemoryProposal[];
  evidence_imports: ContextWikiEvidenceImportRun[];
  promotion_jobs: ContextWikiPromotionJob[];
}

export interface ContextWikiMemoryProposal {
  id: string;
  dedupe_key?: string;
  kind: string;
  classification?: string;
  status: string;
  review_required: boolean;
  confidence: number;
  blast_radius?: string;
  summary: string;
  source_refs?: string[];
  proposed_change?: Record<string, unknown>;
  evaluation_status?: string;
  apply_status?: string;
  count: number;
  created_at: string;
  updated_at: string;
}
