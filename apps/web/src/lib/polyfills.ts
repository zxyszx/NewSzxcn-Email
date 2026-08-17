type FindLastPredicate<T> = (value: T, index: number, array: T[]) => unknown

const arrayPrototype = Array.prototype as {
  findLast?: <T>(this: ArrayLike<T>, predicate: FindLastPredicate<T>, thisArg?: unknown) => T | undefined
  findLastIndex?: <T>(this: ArrayLike<T>, predicate: FindLastPredicate<T>, thisArg?: unknown) => number
}

function toArrayLike<T>(value: ArrayLike<T> | null | undefined) {
  if (value == null) throw new TypeError("Array.prototype method called on null or undefined")
  return Object(value) as ArrayLike<T>
}

function toLength(value: unknown) {
  const length = Number(value)
  if (!isFinite(length) || length <= 0) return 0
  return Math.min(Math.floor(length), 0x1fffffffffffff)
}

if (!arrayPrototype.findLast) {
  Object.defineProperty(Array.prototype, "findLast", {
    configurable: true,
    writable: true,
    value: function findLast<T>(this: ArrayLike<T> | null | undefined, predicate: FindLastPredicate<T>, thisArg?: unknown) {
      if (typeof predicate !== "function") throw new TypeError("predicate must be a function")
      const array = toArrayLike(this)
      const callbackArray = array as T[]
      for (let index = toLength(array.length) - 1; index >= 0; index -= 1) {
        const value = array[index]
        if (predicate.call(thisArg, value, index, callbackArray)) return value
      }
      return undefined
    },
  })
}

if (!arrayPrototype.findLastIndex) {
  Object.defineProperty(Array.prototype, "findLastIndex", {
    configurable: true,
    writable: true,
    value: function findLastIndex<T>(this: ArrayLike<T> | null | undefined, predicate: FindLastPredicate<T>, thisArg?: unknown) {
      if (typeof predicate !== "function") throw new TypeError("predicate must be a function")
      const array = toArrayLike(this)
      const callbackArray = array as T[]
      for (let index = toLength(array.length) - 1; index >= 0; index -= 1) {
        if (predicate.call(thisArg, array[index], index, callbackArray)) return index
      }
      return -1
    },
  })
}

export {}
