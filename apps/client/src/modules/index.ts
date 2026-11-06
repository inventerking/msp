import { manifest as leadGeneration } from "./lead-generation/manifest";
import { manifest as competitorIntelligence } from "./competitor-intelligence/manifest";
import { manifest as outreach } from "./outreach/manifest";
import { manifest as visitorIntelligence } from "./visitor-intelligence/manifest";
import { manifest as chatbot } from "./chatbot/manifest";
import { manifest as crm } from "./crm/manifest";
import { manifest as itAssessment } from "./it-assessment/manifest";
import { manifest as proposals } from "./proposals/manifest";
import { manifest as customerExpansion } from "./customer-expansion/manifest";
import { manifest as salesIntelligence } from "./sales-intelligence/manifest";
import type { ModuleManifest } from "./types";

/** The ten feature modules. Each is lazy-loaded, so a workspace does not download what it lacks. */
export const modules: ModuleManifest[] = [
  leadGeneration,
  competitorIntelligence,
  outreach,
  visitorIntelligence,
  chatbot,
  crm,
  itAssessment,
  proposals,
  customerExpansion,
  salesIntelligence,
];
