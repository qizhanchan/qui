// Package reactive adds a declarative, React-style runtime on top of qui.
//
// The runtime reconciles Element trees into retained widgets and reuses
// instances by kind+key, so dynamic UIs can be written declaratively while
// still taking advantage of qui's retained renderer and dirty-region updates.
package reactive
