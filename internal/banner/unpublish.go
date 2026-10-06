package banner

// UnpublishPlaceholderHTML is what "vx banner --unpublish" writes
// over a page's content. There is no delete endpoint on ht-ml.app (or in
// the share contract), so "unpublishing" is really a PUT
// of this placeholder behind a fresh, immediately-discarded password -
// the URL keeps responding, just to a page nobody can open. Republishing
// the same site_id later (with the same update_key) replaces this
// placeholder and makes the original content visible again.
const UnpublishPlaceholderHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Page withdrawn</title>
<meta name="robots" content="noindex">
</head>
<body>
<p>The owner took this page down and it is no longer available.</p>
</body>
</html>
`
