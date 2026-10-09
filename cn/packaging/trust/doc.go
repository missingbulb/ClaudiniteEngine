// Package trust holds the embedded roots every signature the engine checks
// chains to: the key ceremony's root and standby root, and in a devroots
// build the development roots after them. The release manifest, the pack
// indexes and archives, and an update's pin are all verified against
// Roots.
package trust
