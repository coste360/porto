// Package lan is the local-network mode of the desktop edition (m6a-plan §2.10-§2.12): the self-generated local CA
// and leaf certificate (private keys encrypted with the KeyRing, never in clear on disk), the private-peer
// listener, the address choice, the Host allow-list and the TLS server that implements desktopport.LANController.
// The listener exists only while the owner keeps the local network enabled (INV-79).
//
// It imports desktopport, crypto, clock and reqctx, never the store: the HTTP handler it serves is the app's.
package lan
