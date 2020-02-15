size = 2 * 1024 * 1024

run proc { |env|
  [
    200,
    { 'Content-Type' => 'text/plain', 'Content-Length' => size },
    (size / 1024).times.map { 'x' * 1024 },
  ]
}
