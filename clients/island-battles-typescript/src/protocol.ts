import { Ajv2020 } from "ajv/dist/2020.js";
import addFormats from "ajv-formats";
import { protocol } from "./errors.js";
import type { components } from "./generated/api.js";
import schemas from "./generated/schemas.json" with { type: "json" };

const ajv = new Ajv2020({
  strict: true,
  strictNumbers: true,
  coerceTypes: false,
});
addFormats.default(ajv);
// OpenAPI numeric formats must remain finite and integers safely representable in JS.
ajv.addFormat("int64", { type: "number", validate: Number.isSafeInteger });
ajv.addFormat("double", { type: "number", validate: Number.isFinite });
ajv.addSchema(schemas, "game");
const stateValidator = ajv.compile({ $ref: "game#/$defs/GenerationState" });
const eventValidator = ajv.compile({ $ref: "game#/$defs/GameNextGeneration" });
const tokenValidator = ajv.compile({ $ref: "game#/$defs/ConnectionToken" });

export function parseState(
  data: unknown,
): components["schemas"]["GenerationState"] {
  if (!stateValidator(data)) throw protocol("invalid generation state");
  return data as components["schemas"]["GenerationState"];
}
export function parseEvent(
  data: unknown,
): components["schemas"]["GameNextGeneration"] {
  if (typeof data === "string") {
    try {
      data = JSON.parse(data);
    } catch {
      throw protocol("invalid event JSON");
    }
  }
  if (!eventValidator(data)) throw protocol("invalid generation event");
  return data as components["schemas"]["GameNextGeneration"];
}
export function parseToken(
  data: unknown,
): components["schemas"]["ConnectionToken"] {
  if (!tokenValidator(data)) throw protocol("invalid connection token");
  return data as components["schemas"]["ConnectionToken"];
}
