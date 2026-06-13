// Package image implements image operations for mini-nerdctl.
// It wraps containerd's ImageService and ContentStore to provide:
//   - Pull: resolve + download + unpack an image from a registry
//   - List (images): enumerate all stored images with size and tag info
//   - Delete (rmi): remove an image and optionally its layers
package image
