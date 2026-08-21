#!/usr/bin/env swift

import CoreGraphics
import Foundation
import ImageIO
import UniformTypeIdentifiers

let canvasSize = 1_024
let outputPath = CommandLine.arguments.dropFirst().first ??
    "ios/App/App/Assets.xcassets/AppIcon.appiconset/AppIcon-512@2x.png"

guard let context = CGContext(
    data: nil,
    width: canvasSize,
    height: canvasSize,
    bitsPerComponent: 8,
    bytesPerRow: canvasSize * 4,
    space: CGColorSpace(name: CGColorSpace.sRGB)!,
    bitmapInfo: CGImageAlphaInfo.noneSkipLast.rawValue |
        CGBitmapInfo.byteOrder32Big.rawValue
) else {
    fatalError("Unable to create the AppIcon drawing context")
}

let scale = CGFloat(canvasSize) / 32
func point(_ x: CGFloat, _ y: CGFloat) -> CGPoint {
    CGPoint(x: x * scale, y: (32 - y) * scale)
}

// The iOS icon is the Web favicon's #117864 field and M path. The background
// is full bleed because iOS supplies the final rounded application-icon mask.
context.setFillColor(red: 17 / 255, green: 120 / 255, blue: 100 / 255, alpha: 1)
context.fill(CGRect(x: 0, y: 0, width: canvasSize, height: canvasSize))

let mark = CGMutablePath()
mark.move(to: point(8, 23))
mark.addLine(to: point(8, 9))
mark.addLine(to: point(12, 9))
mark.addLine(to: point(16, 16))
mark.addLine(to: point(20, 9))
mark.addLine(to: point(24, 9))
mark.addLine(to: point(24, 23))
mark.addLine(to: point(20, 23))
mark.addLine(to: point(20, 15))
mark.addLine(to: point(16, 21))
mark.addLine(to: point(12, 15))
mark.addLine(to: point(12, 23))
mark.closeSubpath()
context.addPath(mark)
context.setFillColor(red: 1, green: 1, blue: 1, alpha: 1)
context.fillPath()

guard let image = context.makeImage() else {
    fatalError("Unable to render the AppIcon")
}

let outputURL = URL(fileURLWithPath: outputPath)
guard let destination = CGImageDestinationCreateWithURL(
    outputURL as CFURL,
    UTType.png.identifier as CFString,
    1,
    nil
) else {
    fatalError("Unable to create the AppIcon PNG destination")
}
CGImageDestinationAddImage(destination, image, nil)
guard CGImageDestinationFinalize(destination) else {
    fatalError("Unable to write the AppIcon PNG")
}
