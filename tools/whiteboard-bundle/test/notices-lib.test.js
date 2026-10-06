// The pure helpers behind generate-notices.js: license classification,
// copyright extraction and the woff2 name-table reader. They need no
// node_modules, so this suite runs wherever node does.
import assert from "node:assert/strict";
import test from "node:test";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import {
  classifyLicense,
  copyrightLines,
  declaredLicense,
  licenseBody,
  licenseIds,
  packageRoot,
  readWoff2Names,
  spdxIds,
} from "../notices-lib.js";

const MIT = `Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction.`;
const ISC = `Permission to use, copy, modify, and/or distribute this software for any purpose
with or without fee is hereby granted, provided that the above copyright notice
and this permission notice appear in all copies.`;

test("classifyLicense tells the common licenses apart", () => {
  assert.equal(classifyLicense(`(The MIT License)\n\nCopyright (c) 2020 A\n\n${MIT}`), "MIT");
  assert.equal(classifyLicense(`Copyright 2020 A\n\n${ISC}`), "ISC");
  assert.equal(
    classifyLicense("Permission to use, copy, modify, and/or distribute this software for any purpose with or without fee is hereby granted."),
    "0BSD",
  );
  assert.equal(classifyLicense("Redistribution and use in source and binary forms, with or without modification, are permitted ..."), "BSD-2-Clause");
  assert.equal(
    classifyLicense("Redistribution and use in source and binary forms ... Neither the name of the copyright holder nor the names of its contributors ..."),
    "BSD-3-Clause",
  );
  assert.equal(classifyLicense("Apache License\nVersion 2.0, January 2004"), "Apache-2.0");
  assert.equal(classifyLicense("Mozilla Public License Version 2.0\n=="), "MPL-2.0");
  assert.equal(classifyLicense("2. Altered source versions must be plainly marked as such"), "Zlib");
  assert.equal(classifyLicense("This is free and unencumbered software released into the public domain."), "Unlicense");
  assert.equal(classifyLicense("some home-grown terms"), null);
});

test("a file that holds two licenses is not one license", () => {
  const both = `Copyright 2010 A\n\n${ISC}\n\nThis license applies to B.\n\nCopyright 2008 B\n\n${MIT}`;
  assert.deepEqual(licenseIds(both), ["ISC", "MIT"]);
  assert.equal(classifyLicense(both), null);
});

test("copyrightLines keeps holders and drops boilerplate and templates", () => {
  const text = [
    "Copyright (c) 2014 - 2022 Knut Sveidqvist",
    " * (C) 1995-2013 Jean-loup Gailly and Mark Adler",
    "The above copyright notice and this permission notice shall be included",
    "   Copyright [yyyy] [name of copyright owner]",
    "   2. Grant of Copyright License. Subject to the terms",
    "   (c) You must retain, in the Source form of any Derivative Works",
    "Copyright (c) 2012 [Aaron Heckmann](aaron@example.com)",
    "Copyright and related rights for sample code are waived via CC0.",
    "Copyright (c) 2014 - 2022 Knut Sveidqvist",
  ].join("\n");
  assert.deepEqual(copyrightLines(text), [
    "Copyright (c) 2014 - 2022 Knut Sveidqvist",
    "(C) 1995-2013 Jean-loup Gailly and Mark Adler",
    "Copyright (c) 2012 [Aaron Heckmann](aaron@example.com)",
  ]);
});

test("licenseBody ignores copyright holders, title and whitespace", () => {
  const a = licenseBody(`MIT License\n\nCopyright (c) 2020 A\n\n${MIT}`);
  const b = licenseBody(`(The MIT License)\nCopyright (c) 1999 B\n\n${MIT.replace(/\n/g, "\n  ")}`);
  assert.equal(a, b);
});

test("spdxIds and declaredLicense read expressions and legacy fields", () => {
  assert.deepEqual(spdxIds("(MPL-2.0 OR Apache-2.0)"), ["MPL-2.0", "Apache-2.0"]);
  assert.deepEqual(spdxIds("MIT AND Zlib"), ["MIT", "Zlib"]);
  assert.equal(declaredLicense({ license: "ISC" }), "ISC");
  assert.equal(declaredLicense({ license: { type: "MIT" } }), "MIT");
  assert.equal(declaredLicense({ licenses: [{ type: "MIT", url: "x" }] }), "MIT");
  assert.equal(declaredLicense({ name: "x" }), null);
});

test("packageRoot finds the innermost package, scoped or nested", () => {
  assert.equal(packageRoot("node_modules/react/index.js"), "node_modules/react");
  assert.equal(packageRoot("node_modules/@radix-ui/react-slot/dist/index.mjs"), "node_modules/@radix-ui/react-slot");
  assert.equal(packageRoot("node_modules/a/node_modules/@s/b/x.js"), "node_modules/a/node_modules/@s/b");
  assert.equal(packageRoot("src/whiteboard-frame.js"), null);
});

test("readWoff2Names reads the name table of a vendored font", () => {
  const here = dirname(fileURLToPath(import.meta.url));
  const names = readWoff2Names(join(here, "../../../internal/forum/assets/whiteboard/fonts/Virgil/Virgil-Regular.woff2"));
  assert.equal(names[1], "Virgil");
  assert.match(names[0], /Your Own Font Foundry/);
  assert.match(names[13], /SIL OPEN FONT LICENSE Version 1\.1/);
});
