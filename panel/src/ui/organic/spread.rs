use iced::Size;

use super::Field;

/// A field stretched smooth to the size of a surface, with the surface's
/// rounded corners cut out of it: the picture that is drawn.
///
/// It is stretched here and not by the renderer because the software
/// renderer places a picture it enlarges on whole pixels *of the
/// picture* (a 56 pixel field over a 1300 pixel row would land up to
/// 23 pixels to the left) and rounds no picture's corners. At one pixel
/// of picture for each point of surface both renderers draw it where it
/// belongs, and the corners are part of the picture.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct Spread {
    width: u32,
    height: u32,
    /// Points of the surface for each pixel of the picture.
    step: f32,
    surface: Size,
    radius: f32,
}

impl Spread {
    /// The most pixels a picture has. A bigger surface gets one pixel
    /// for every two points, or four: the picture stays well under the
    /// size the GPU renderer uploads at once, without waiting a frame.
    pub const MOST: f32 = 400_000.0;

    /// The picture for a surface of this size, in points, with corners of
    /// this radius. None for a surface that is not there.
    pub fn over(surface: Size, radius: f32) -> Option<Self> {
        if !(surface.width >= 1.0 && surface.height >= 1.0) {
            return None;
        }
        let mut step = 1.0;
        while (surface.width / step).ceil() * (surface.height / step).ceil() > Self::MOST {
            step *= 2.0;
        }
        // One pixel more each way: the software renderer may still place
        // the picture a fraction of a pixel up and to the left, and the
        // far edges must stay covered. What sticks out is clipped away.
        Some(Self {
            width: (surface.width / step).ceil() as u32 + 1,
            height: (surface.height / step).ceil() as u32 + 1,
            step,
            surface,
            radius: radius.clamp(0.0, surface.width.min(surface.height) / 2.0),
        })
    }

    pub fn width(&self) -> u32 {
        self.width
    }

    pub fn height(&self) -> u32 {
        self.height
    }

    /// The points the picture takes on the screen, from the surface's top
    /// left corner: the surface and a little more.
    pub fn covers(&self) -> Size {
        Size::new(self.width as f32 * self.step, self.height as f32 * self.step)
    }

    /// The field's pixels (as `Field::pixels` gives them) stretched to
    /// the picture's, each blended from the four around it.
    pub fn stretch(&self, field: Field, pixels: &[u8]) -> Vec<u8> {
        let (width, height) = (self.width as usize, self.height as usize);
        let (columns, rows) = (field.width as usize, field.height as usize);
        assert_eq!(pixels.len(), columns * rows * 4, "the field's pixels");
        let across: Vec<(usize, usize, f32)> = (0..width)
            .map(|x| between((x as f32 + 0.5) * self.step / self.surface.width, columns))
            .collect();
        // Every row of the field stretched across first; a row of the
        // picture is then a blend of two of them.
        let mut bands = Vec::with_capacity(rows * width * 4);
        for row in pixels.chunks_exact(columns * 4) {
            for &(left, right, share) in &across {
                for channel in 0..4 {
                    let (a, b) = (f32::from(row[left * 4 + channel]), f32::from(row[right * 4 + channel]));
                    bands.push(a + (b - a) * share);
                }
            }
        }
        let mut picture = Vec::with_capacity(width * height * 4);
        for y in 0..height {
            let (above, below, share) = between((y as f32 + 0.5) * self.step / self.surface.height, rows);
            let above = &bands[above * width * 4..(above + 1) * width * 4];
            let below = &bands[below * width * 4..(below + 1) * width * 4];
            picture.extend(above.iter().zip(below).map(|(a, b)| (a + (b - a) * share + 0.5) as u8));
        }
        self.round_corners(&mut picture);
        picture
    }

    /// Clears the picture outside the surface's rounded corners, with a
    /// soft edge one pixel wide.
    fn round_corners(&self, picture: &mut [u8]) {
        let radius = self.radius;
        if radius <= 0.0 {
            return;
        }
        // The centre of the corner's circle on this axis, for a place
        // that is in a corner.
        let centre = |at: f32, length: f32| {
            if at < radius {
                Some(radius)
            } else if at > length - radius {
                Some(length - radius)
            } else {
                None
            }
        };
        for y in 0..self.height as usize {
            let down = (y as f32 + 0.5) * self.step;
            let Some(centre_y) = centre(down, self.surface.height) else {
                continue;
            };
            for x in 0..self.width as usize {
                let along = (x as f32 + 0.5) * self.step;
                let Some(centre_x) = centre(along, self.surface.width) else {
                    continue;
                };
                let inside = radius - (along - centre_x).hypot(down - centre_y);
                let cover = (inside / self.step + 0.5).clamp(0.0, 1.0);
                let alpha = &mut picture[(y * self.width as usize + x) * 4 + 3];
                *alpha = (f32::from(*alpha) * cover + 0.5) as u8;
            }
        }
    }
}

/// The two pixels of a line of `count` that the place `at` (0 to 1 along
/// it) lies between, and how far it is from the first to the second.
fn between(at: f32, count: usize) -> (usize, usize, f32) {
    let place = (at * count as f32 - 0.5).clamp(0.0, (count - 1) as f32);
    let first = place.floor() as usize;
    (first, (first + 1).min(count - 1), place - first as f32)
}

#[cfg(test)]
mod tests {
    use iced::Point;

    use super::*;
    use crate::ui::Tokens;
    use crate::ui::organic::{Glow, Halo, Tint};

    fn alpha(spread: &Spread, picture: &[u8], x: u32, y: u32) -> u8 {
        picture[((y * spread.width() + x) * 4 + 3) as usize]
    }

    /// A field of one flat colour, half clear.
    fn flat(field: Field) -> Vec<u8> {
        [10, 20, 30, 128].repeat((field.width * field.height) as usize)
    }

    fn lit(pointer: Point, aspect: f32) -> Vec<u8> {
        Field::SMALL.pixels(&Glow {
            aspect,
            time: 2.0,
            pointer,
            tint: Tint::of(Tokens::LIGHT),
            halo: Halo::of(Size::new(aspect * 300.0, 300.0)),
            fade: 1.0,
        })
    }

    #[test]
    fn has_a_pixel_for_each_point_of_the_surface_and_one_more() {
        let spread = Spread::over(Size::new(640.0, 300.0), 8.0).unwrap();
        assert_eq!((spread.width(), spread.height()), (641, 301));
        assert_eq!(spread.covers(), Size::new(641.0, 301.0));
        let uneven = Spread::over(Size::new(412.5, 36.0), 0.0).unwrap();
        assert_eq!((uneven.width(), uneven.height()), (414, 37));
        let picture = spread.stretch(Field::SMALL, &flat(Field::SMALL));
        assert_eq!(picture.len(), 641 * 301 * 4);
    }

    #[test]
    fn a_big_surface_gets_fewer_pixels_than_points() {
        let window = Size::new(1360.0, 860.0);
        let spread = Spread::over(window, 8.0).unwrap();
        assert_eq!((spread.width(), spread.height()), (681, 431));
        assert!(spread.covers().width >= window.width && spread.covers().height >= window.height);
        for size in [window, Size::new(3840.0, 2160.0), Size::new(20_000.0, 40.0)] {
            let spread = Spread::over(size, 0.0).unwrap();
            assert!(spread.width() * spread.height() * 4 < 2 * 1024 * 1024, "{size:?}");
        }
    }

    #[test]
    fn there_is_no_picture_for_a_surface_that_is_not_there() {
        assert_eq!(Spread::over(Size::new(0.0, 36.0), 0.0), None);
        assert_eq!(Spread::over(Size::new(200.0, 0.5), 0.0), None);
        assert_eq!(Spread::over(Size::new(f32::NAN, 36.0), 0.0), None);
    }

    #[test]
    fn a_flat_field_stays_flat() {
        let spread = Spread::over(Size::new(200.0, 80.0), 0.0).unwrap();
        let picture = spread.stretch(Field::SMALL, &flat(Field::SMALL));
        assert!(picture.chunks_exact(4).all(|pixel| pixel == [10, 20, 30, 128]));
    }

    /// No blocks and no hard edge: from one pixel to the next the
    /// opacity moves by a step or two of its 255.
    #[test]
    fn the_glow_is_smooth_from_pixel_to_pixel() {
        for (size, pointer) in [
            (Size::new(640.0, 300.0), Point::new(0.4, 0.5)),
            (Size::new(1290.0, 36.0), Point::new(0.7, 0.5)),
            (Size::new(140.0, 24.0), Point::new(0.5, 0.5)),
        ] {
            let spread = Spread::over(size, 0.0).unwrap();
            let picture = spread.stretch(Field::SMALL, &lit(pointer, size.width / size.height));
            let mut steepest = 0;
            for y in 0..spread.height() {
                for x in 1..spread.width() {
                    let step = alpha(&spread, &picture, x, y).abs_diff(alpha(&spread, &picture, x - 1, y));
                    steepest = steepest.max(step);
                }
            }
            assert!(steepest <= 3, "a step of {steepest} on {size:?}");
            assert!(
                picture.chunks_exact(4).any(|pixel| pixel[3] > 20),
                "it shows on {size:?}"
            );
        }
    }

    #[test]
    fn the_glow_stays_under_the_pointer() {
        let size = Size::new(640.0, 300.0);
        let spread = Spread::over(size, 8.0).unwrap();
        let pointer = Point::new(0.75, 0.3);
        let picture = spread.stretch(Field::SMALL, &lit(pointer, size.width / size.height));
        let (mut sum, mut sum_x, mut sum_y) = (0.0, 0.0, 0.0);
        for y in 0..spread.height() {
            for x in 0..spread.width() {
                let weight = f32::from(alpha(&spread, &picture, x, y));
                sum += weight;
                sum_x += weight * x as f32;
                sum_y += weight * y as f32;
            }
        }
        let (centre_x, centre_y) = (sum_x / sum, sum_y / sum);
        assert!((centre_x - pointer.x * size.width).abs() < 30.0, "centre at {centre_x}");
        assert!(
            (centre_y - pointer.y * size.height).abs() < 30.0,
            "centre at {centre_y}"
        );
    }

    #[test]
    fn the_corners_are_cut_round() {
        let size = Size::new(200.0, 80.0);
        let field = flat(Field::SMALL);
        let round = Spread::over(size, 8.0).unwrap();
        let picture = round.stretch(Field::SMALL, &field);
        for (x, y) in [(0, 0), (1, 1), (199, 0), (0, 79), (199, 79), (198, 78)] {
            assert_eq!(alpha(&round, &picture, x, y), 0, "outside the corner at {x},{y}");
        }
        // On the circle's edge it is half there; inside it, whole.
        for (x, y) in [
            (8, 0),
            (0, 8),
            (4, 4),
            (100, 0),
            (0, 40),
            (195, 75),
            (191, 79),
            (100, 79),
        ] {
            assert_eq!(alpha(&round, &picture, x, y), 128, "inside the corner at {x},{y}");
        }
        let edge = alpha(&round, &picture, 2, 2);
        assert!(edge > 0 && edge < 128, "a soft edge, {edge}");
        // The pixel past the surface's far corner is clear as well.
        assert_eq!(alpha(&round, &picture, 200, 80), 0);

        let square = Spread::over(size, 0.0).unwrap();
        let picture = square.stretch(Field::SMALL, &field);
        assert_eq!(alpha(&square, &picture, 0, 0), 128);
    }

    #[test]
    fn a_radius_is_never_more_than_half_the_surface() {
        let pill = Spread::over(Size::new(100.0, 24.0), 99.0).unwrap();
        let picture = pill.stretch(Field::SMALL, &flat(Field::SMALL));
        assert_eq!(alpha(&pill, &picture, 50, 12), 128);
        assert_eq!(alpha(&pill, &picture, 0, 0), 0);
        assert_eq!(alpha(&pill, &picture, 1, 12), 128);
    }
}
