#ifndef RUST_CALCULATOR_H
#define RUST_CALCULATOR_H

#include <stdint.h>

/*
 * Declared manually because Rust does not generate a C header on its own.
 * Safe to declare like this because lib.rs exports `sub` as
 * `extern "C" fn` with `#[no_mangle]`, which gives it the standard C ABI
 * and an unmangled symbol name.
 */
int64_t sub(int64_t a, int64_t b);

#endif
